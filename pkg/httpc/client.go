package httpc

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/aristosMiliaressis/go-ip-rotate/pkg/iprotate"
	"github.com/aristosMiliaressis/httpc/internal/util"
	"github.com/corpix/uarand"
	"github.com/projectdiscovery/gologger"
	"github.com/projectdiscovery/rawhttp"
)

type HttpClient struct {
	context    context.Context
	cancel     context.CancelFunc
	client     http.Client
	Options    ClientOptions
	ThreadPool *ThreadPool

	MessageLog MessageLog

	cookieJar      map[string]string
	cookieJarMutex sync.RWMutex

	errorLog   map[string]int
	errorMutex sync.Mutex

	totalErrors       int
	totalSuccessful   int
	consecutiveErrors int

	apiGateways     map[string]*iprotate.ApiEndpoint
	apiGatewayMutex sync.Mutex
}

func NewHttpClient(opts ClientOptions, ctx context.Context) *HttpClient {
	ctx, cancel := context.WithCancel(ctx)

	c := HttpClient{
		context:   ctx,
		cancel:    cancel,
		Options:   opts,
		client:    createInternalHttpClient(opts),
		errorLog:  map[string]int{},
		cookieJar: map[string]string{},
	}

	c.ThreadPool = NewThreadPool(c.handleResponse, ctx, opts.Performance.RequestsPerSecond, 1000)
	go c.ThreadPool.Run()

	return &c
}

func (c *HttpClient) Close() {
	c.apiGatewayMutex.Lock()
	for k := range c.apiGateways {
		c.apiGateways[k].Delete()
	}
	c.apiGatewayMutex.Unlock()

	for k := range c.ThreadPool.queuePriorityMap {
		close(c.ThreadPool.queuePriorityMap[k])
	}
}

func (c *HttpClient) Send(req *http.Request) *MessageDuplex {
	return c.SendWithOptions(req, c.Options)
}

func (c *HttpClient) SendWithOptions(req *http.Request, opts ClientOptions) *MessageDuplex {

	c.sleepIfNeeded(opts.Performance.Delay)

	msg := &MessageDuplex{
		Request:  req.Clone(c.context),
		Resolved: make(chan bool, 1),
	}

	if len(c.apiGateways) > 0 {
		c.apiGatewayMutex.Lock()
		for gateway := range c.apiGateways {
			if strings.Contains(req.URL.String(), gateway) {
				replacedUrl := strings.Replace(req.URL.String(), gateway, c.apiGateways[gateway].ProxyUrl, 1)
				gatewayUrl, err := url.Parse(replacedUrl)
				if err != nil {
					gologger.Fatal().Msgf("failed to update url to ip-rotate url")
				}
				msg.Request.URL = gatewayUrl
			}
		}
		c.apiGatewayMutex.Unlock()
	}

	if c.Options.SimulateBrowserRequests {
		util.SimulateBrowserRequest(msg.Request)
	}

	if c.Options.RandomizeUserAgent {
		msg.Request.Header.Set("User-Agent", uarand.GetRandom())
	}

	for k, v := range c.Options.DefaultHeaders {
		msg.Request.Header.Set(k, v)
	}

	for k, v := range msg.Request.Header {
		msg.Request.Header.Set(k, v[0])
	}

	if msg.Request.ProtoMajor == 2 {
		msg.Request.Header.Del("Connection")
		msg.Request.Header.Del("Upgrade")
		msg.Request.Header.Del("Transfer-Encoding")
	}

	for k, v := range c.GetCookieJar() {
		if ContainsCookie(msg.Request, k) || util.Contains(opts.ExcludeCookies, k) {
			continue
		}
		msg.Request.AddCookie(&http.Cookie{Name: k, Value: v})
	}

	opts.CacheBusting.Apply(msg.Request)

	var start time.Time
	trace := &httptrace.ClientTrace{
		WroteRequest: func(_ httptrace.WroteRequestInfo) {
			// begin the timer after the request is fully written
			start = time.Now()
		},
		GotFirstResponseByte: func() {
			// record when the first byte of the response was received
			msg.Duration = time.Since(start)
		},
	}

	msg.Request = msg.Request.WithContext(httptrace.WithClientTrace(c.context, trace))

	c.ThreadPool.queuePriorityMutex.RLock()
	queue, ok := c.ThreadPool.queuePriorityMap[opts.RequestPriority]
	c.ThreadPool.queuePriorityMutex.RUnlock()

	if !ok {
		queue = c.ThreadPool.NewRequestQueue()
		c.ThreadPool.queuePriorityMutex.Lock()
		c.ThreadPool.queuePriorityMap[opts.RequestPriority] = queue
		c.ThreadPool.queuePriorityMutex.Unlock()
	}

	queue <- PendingRequest{"", msg, opts}

	return msg
}

func (c *HttpClient) SendRaw(rawreq string, baseUrl string) *MessageDuplex {
	return c.SendRawWithOptions(rawreq, baseUrl, c.Options)
}

func (c *HttpClient) SendRawWithOptions(rawreq string, baseUrl string, opts ClientOptions) *MessageDuplex {

	c.sleepIfNeeded(opts.Performance.Delay)

	msg := &MessageDuplex{
		Resolved: make(chan bool, 1),
	}
	msg.Request, _ = http.NewRequest("GET", baseUrl, nil)

	c.ThreadPool.queuePriorityMutex.RLock()
	queue, ok := c.ThreadPool.queuePriorityMap[opts.RequestPriority]
	c.ThreadPool.queuePriorityMutex.RUnlock()

	if !ok {
		queue = c.ThreadPool.NewRequestQueue()
		c.ThreadPool.queuePriorityMutex.Lock()
		c.ThreadPool.queuePriorityMap[opts.RequestPriority] = queue
		c.ThreadPool.queuePriorityMutex.Unlock()
	}

	queue <- PendingRequest{rawreq, msg, opts}

	return msg
}

func (c *HttpClient) ConnectRequest(proxyUrl *url.URL, destUrl *url.URL, opts ClientOptions) *MessageDuplex {
	msg := MessageDuplex{}
	c.MessageLog = append(c.MessageLog, &msg)

	proxyAddr := proxyUrl.Host
	if proxyUrl.Port() == "" {
		if proxyUrl.Scheme == "http" {
			proxyAddr = net.JoinHostPort(proxyAddr, "80")
		} else {
			proxyAddr = net.JoinHostPort(proxyAddr, "443")
		}
	}

	if destUrl.Port() == "" {
		if proxyUrl.Scheme == "http" {
			destUrl.Host = destUrl.Host + ":80"
		} else {
			destUrl.Host = destUrl.Host + ":443"
		}
	}

	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		gologger.Error().Msgf("dialing proxy %s failed: %v", proxyAddr, err)
		return &msg
	}
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: basic aGVsbG86d29ybGQ=\r\n\r\n", destUrl.Host, destUrl.Host)
	br := bufio.NewReader(conn)
	msg.Response, err = http.ReadResponse(br, nil)
	if err != nil {
		// connect check failed, ignore error
		return &msg
	}
	// It's safe to discard the bufio.Reader here and return the
	// original TCP conn directly because we only use this for
	// TLS, and in TLS the client speaks first, so we know there's
	// no unbuffered data. But we can double-check.
	if br.Buffered() > 0 {
		gologger.Error().Msgf("unexpected %d bytes of buffered data from CONNECT proxy %q", br.Buffered(), proxyAddr)
	}
	return &msg
}

func createInternalHttpClient(opts ClientOptions) http.Client {
	proxyURL := http.ProxyFromEnvironment
	if len(opts.Connection.ProxyUrl) > 0 {
		pu, err := url.Parse(opts.Connection.ProxyUrl)
		if err == nil {
			proxyURL = http.ProxyURL(pu)
		}
	}

	if opts.Connection.ForceAttemptHTTP1 {
		os.Setenv("GODEBUG", "http2client=0")
	}

	return http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       time.Duration(time.Duration(opts.Performance.Timeout) * time.Second),
		Transport: &http.Transport{
			Proxy:               proxyURL,
			ForceAttemptHTTP2:   opts.Connection.ForceAttemptHTTP2,
			DisableKeepAlives:   opts.Connection.DisableKeepAlives,
			DisableCompression:  true,
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 500,
			MaxConnsPerHost:     500,
			DialContext: (&net.Dialer{
				Timeout: time.Duration(time.Duration(opts.Performance.Timeout) * time.Second),
			}).DialContext,
			TLSHandshakeTimeout: time.Duration(time.Duration(opts.Performance.Timeout) * time.Second),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				MinVersion:         tls.VersionTLS10,
				Renegotiation:      tls.RenegotiateOnceAsClient,
				ServerName:         opts.Connection.SNI,
			},
		},
	}
}

func (c *HttpClient) sleepIfNeeded(delay Range) {

	sTime := delay.Min + rand.Float64()*(delay.Max-delay.Min)
	sleepDuration, _ := time.ParseDuration(fmt.Sprintf("%dms", int(sTime*1000)))

	select {
	case <-c.context.Done():
	case <-time.After(sleepDuration):
	}
}

func (c *HttpClient) handleResponse(uow PendingRequest) {
	defer func() { uow.Message.Resolved <- true }()

	var sendErr error
	if uow.RawRequest == "" {
		if uow.Options.Connection.SNI != "" {
			sniClient := createInternalHttpClient(uow.Options)

			uow.Message.Response, sendErr = sniClient.Do(uow.Message.Request)
		} else {
			uow.Message.Response, sendErr = c.client.Do(uow.Message.Request)
		}
	} else {
		rawhttpOptions := rawhttp.DefaultOptions
		rawhttpOptions.AutomaticHostHeader = false
		rawhttpOptions.CustomRawBytes = []byte(uow.RawRequest)
		httpclient := rawhttp.NewClient(rawhttpOptions)
		defer httpclient.Close()

		uow.Message.Response, sendErr = httpclient.DoRaw("GET", uow.Message.Request.URL.String(), "", nil, nil)
	}

	c.MessageLog = append(c.MessageLog, uow.Message)

	// handle transport errors
	if sendErr != nil {
		c.handleTransportError(uow.Message, sendErr)
		return
	}

	if uow.Message.Response == nil && uow.Options.ErrorHandling.RetryTransportFailures {
		if uow.RawRequest == "" {
			retriedMsg := c.SendWithOptions(uow.Message.Request, uow.Options)
			*uow.Message = *retriedMsg
		} else {
			retriedMsg := c.SendRawWithOptions(uow.RawRequest, uow.Message.Request.URL.String(), uow.Options)
			*uow.Message = *retriedMsg
		}
		return
	}

	gologger.Debug().Msgf("%s %s %d\n", uow.Message.Request.URL.String(), uow.Message.Response.Status, uow.Message.Response.ContentLength)

	// Update cookie jar
	if c.Options.MaintainCookieJar && uow.Message.Response.Cookies() != nil {
		for _, cookie := range uow.Message.Response.Cookies() {
			c.AddCookie(cookie.Name, cookie.Value)
		}
	}

	var dcprsErr error
	if uow.Message.Response != nil && uow.Message.Response.Body != nil {
		var body []byte
		orig := bytes.NewBuffer(body)
		switch uow.Message.Response.Header.Get("Content-Encoding") {
		case "gzip":
			reader, readErr := gzip.NewReader(uow.Message.Response.Body)
			if readErr == nil {
				defer reader.Close()
				body, dcprsErr = io.ReadAll(reader)
			} else {
				body, dcprsErr = io.ReadAll(orig)
			}
		case "br":
			reader := brotli.NewReader(uow.Message.Response.Body)
			body, dcprsErr = io.ReadAll(reader)
		case "deflate":
			reader := flate.NewReader(uow.Message.Response.Body)
			defer reader.Close()
			body, dcprsErr = io.ReadAll(reader)
		default:
			body, dcprsErr = io.ReadAll(uow.Message.Response.Body)
		}

		uow.Message.Response.Body = io.NopCloser(bytes.NewBuffer(body))
	}

	if dcprsErr != nil {
		gologger.Error().Msgf("Error while reading response %s", dcprsErr)
		return
	}

	// handle http errors
	if uow.Message.TransportError != NoError || (uow.Message.Response.StatusCode >= 400 && !util.Contains(safeErrorsList, uow.Message.Response.StatusCode)) {
		c.totalErrors += 1
		c.consecutiveErrors += 1
		c.handleHttpError(uow.Message)
		return
	} else {
		c.totalSuccessful += 1
		c.consecutiveErrors = 0
	}

	// handle redirects
	if uow.Message.Response.StatusCode >= 300 && uow.Message.Response.StatusCode <= 399 {
		absRedirect := util.GetRedirectLocation(uow.Message.Response)

		uow.Message.CrossOriginRedirect = util.IsCrossOrigin(uow.Message.Request.URL.String(), absRedirect)
		uow.Message.CrossSiteRedirect = util.IsCrossSite(uow.Message.Request.URL.String(), absRedirect)

		if uow.Options.Redirection.PreventCrossOriginRedirects && uow.Message.CrossOriginRedirect {
			return
		}

		if uow.Options.Redirection.PreventCrossSiteRedirects && uow.Message.CrossSiteRedirect {
			return
		}

		uow.Options.Redirection.currentDepth++
		if uow.Options.Redirection.currentDepth > uow.Options.Redirection.MaxRedirects {
			uow.Message.MaxRedirectsExheeded = true
			return
		}

		if !uow.Options.Redirection.FollowRedirects {
			return
		}

		redirectedReq := uow.Message.Request.Clone(c.context)
		redirectedReq.Header.Del("Cookie") // TODO: figure out why did i do this??
		uow.Options.CacheBusting.Clear(redirectedReq)

		absRedirectUrl, _ := url.Parse(absRedirect)
		redirectedReq.Host = absRedirectUrl.Host
		redirectedReq.URL, _ = url.Parse(absRedirect)

		newMsg := c.SendWithOptions(redirectedReq, uow.Options)
		newMsg.AddRedirect(uow.Message)
		<-newMsg.Resolved

		c.MessageLog = append(c.MessageLog, newMsg)

		return
	}

	// handle rate-limitting
	if uow.Message.Response.StatusCode == 429 || uow.Message.Response.StatusCode == 529 {
		if uow.Options.Performance.AutoRateThrottle {
			c.ThreadPool.Rate.ChangeRate(c.ThreadPool.Rate.RPS - 1)
		}

		if uow.Options.Performance.ReplayRateLimitted {
			replayReq := uow.Message.Request.Clone(c.context)
			uow.Message = c.SendWithOptions(replayReq, uow.Options)
		}

		uow.Message.RateLimited = true
	}
}
