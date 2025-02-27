package httpc

import (
	"sync/atomic"
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
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
	ipBanCheck atomic.Bool

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

	c.ThreadPool = NewThreadPool(c.handleMessage, ctx, opts.Performance.RequestsPerSecond, opts.Performance.Delay, 10000)
	go c.ThreadPool.Run()

	return &c
}

func (c *HttpClient) Close() {
	c.cancel()

	c.apiGatewayMutex.Lock()
	for k := range c.apiGateways {
		c.apiGateways[k].Delete()
	}
	c.apiGatewayMutex.Unlock()

	c.ThreadPool.queuePriorityMutex.Lock()
	for p := range c.ThreadPool.queuePriorityMap {
		ch := c.ThreadPool.queuePriorityMap[p]
		delete(c.ThreadPool.queuePriorityMap, p)
		for len(ch) > 0 {
			req := <-ch
			close(req.Message.Resolved)
		}
		close(ch)
	}
	c.ThreadPool.queuePriorityMutex.Unlock()
	close(c.ThreadPool.totalThreads)
	close(c.ThreadPool.lockedThreads)
}

func (c *HttpClient) Send(req *http.Request) *MessageDuplex {
	return c.SendWithOptions(req, c.Options)
}

func (c *HttpClient) SendWithOptions(req *http.Request, opts ClientOptions) *MessageDuplex {

	msg := &MessageDuplex{
		Request:  req.Clone(c.context),
		Resolved: make(chan bool, 1),
	}

	if opts.Connection.EnableIPRotate {
		c.enableIpRotate(msg.Request.URL)
	}

	if len(c.apiGateways) > 0 {
		c.apiGatewayMutex.Lock()
		for gateway := range c.apiGateways {
			if GetBaseUrl(req.URL).String() == gateway {
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

	if opts.SimulateBrowserRequests {
		util.SimulateBrowserRequest(msg.Request)
	}

	if opts.RandomizeUserAgent {
		msg.Request.Header.Set("User-Agent", uarand.GetRandom())
	}

	for k, v := range opts.DefaultHeaders {
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

	c.ThreadPool.queuePriorityMutex.Lock()
	queue, ok := c.ThreadPool.queuePriorityMap[opts.RequestPriority]
	if !ok {
		queue = c.ThreadPool.NewRequestQueue()
		c.ThreadPool.queuePriorityMap[opts.RequestPriority] = queue
	}
	c.ThreadPool.queuePriorityMutex.Unlock()

	select {
	case <-c.context.Done():
		close(msg.Resolved)
		return msg
	default:
		queue <- PendingRequest{"", msg, opts}
	}

	return msg
}

func (c *HttpClient) SendRaw(rawreq string, baseUrl string) *MessageDuplex {
	return c.SendRawWithOptions(rawreq, baseUrl, c.Options)
}

func (c *HttpClient) SendRawWithOptions(rawreq string, baseUrl string, opts ClientOptions) *MessageDuplex {

	msg := &MessageDuplex{
		Resolved: make(chan bool, 1),
	}
	msg.Request, _ = http.NewRequest("GET", baseUrl, nil)

	c.ThreadPool.queuePriorityMutex.Lock()
	queue, ok := c.ThreadPool.queuePriorityMap[opts.RequestPriority]
	if !ok {
		queue = c.ThreadPool.NewRequestQueue()
		c.ThreadPool.queuePriorityMap[opts.RequestPriority] = queue
	}
	c.ThreadPool.queuePriorityMutex.Unlock()

	select {
	case <-c.context.Done():
		close(msg.Resolved)
		return msg
	default:
		queue <- PendingRequest{rawreq, msg, opts}
	}

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
		gologger.Debug().Msgf("dialing proxy %s failed: %v", proxyAddr, err)
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
		gologger.Debug().Msgf("unexpected %d bytes of buffered data from CONNECT proxy %q", br.Buffered(), proxyAddr)
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

func (c *HttpClient) handleMessage(uow PendingRequest) {
	defer func() { uow.Message.Resolved <- true }()

	c.ThreadPool.Rate.SetRatelimitPercentage(c.calculate429Percentage())
	
	var sendErr error
	if uow.RawRequest == "" {
		if uow.Options.Connection.SNI != "" {
			sniClient := createInternalHttpClient(uow.Options)

			uow.Message.Response, sendErr = sniClient.Do(uow.Message.Request)
		} else {
			uow.Message.Response, sendErr = c.client.Do(uow.Message.Request)
		}
	} else {
		opts := c.Options.RawHttp
		opts.CustomRawBytes = []byte(uow.RawRequest)
		httpclient := rawhttp.NewClient(&opts)
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
		uow.Message.Resolved <- true
		if uow.RawRequest == "" {
			retriedMsg := c.SendWithOptions(uow.Message.Request, uow.Options)
			*uow.Message = *retriedMsg
		} else {
			retriedMsg := c.SendRawWithOptions(uow.RawRequest, uow.Message.Request.URL.String(), uow.Options)
			*uow.Message = *retriedMsg
		}
		return
	}

	gologger.Debug().Msgf("URL %s\tStatus: %d\n", uow.Message.Request.URL.String(), uow.Message.Response.StatusCode)
	gologger.Debug().Msg(c.GetErrorSummary())

	// Update cookie jar
	if uow.Options.MaintainCookieJar && uow.Message.Response.Cookies() != nil {
		for _, cookie := range uow.Message.Response.Cookies() {
			c.AddCookie(cookie.Name, cookie.Value)
		}
	}

	var dcprsErr error
	if uow.Message.Response != nil && uow.Message.Response.Body != nil {
		var body []byte
		orig, _ := io.ReadAll(uow.Message.Response.Body)
		switch uow.Message.Response.Header.Get("Content-Encoding") {
		case "gzip":
			reader, readErr := gzip.NewReader(bytes.NewBuffer(orig))
			if readErr == nil {
				defer reader.Close()
				body, dcprsErr = io.ReadAll(reader)
			} else {
				body = orig
			}
		case "br":
			reader := brotli.NewReader(bytes.NewBuffer(orig))
			body, dcprsErr = io.ReadAll(reader)
		case "deflate":
			reader := flate.NewReader(bytes.NewBuffer(orig))
			defer reader.Close()
			body, dcprsErr = io.ReadAll(reader)
		default:
			body, dcprsErr = io.ReadAll(bytes.NewBuffer(orig))
		}

		uow.Message.Response.Body = io.NopCloser(bytes.NewBuffer(body))
		uow.Message.Response.ContentLength = int64(len(body))
	}

	if dcprsErr != nil {
		gologger.Debug().Msgf("Error while reading response %s", dcprsErr)
		return
	}

	// handle http errors
	if uow.Message.TransportError != NoError || (uow.Message.Response.StatusCode >= 400 && uow.Options.ErrorHandling.Matches(uow.Message.Response.StatusCode)) {
		c.totalErrors += 1
		c.consecutiveErrors += 1
		c.handleHttpError(uow.Message)
	} else {
		c.totalSuccessful += 1
		c.consecutiveErrors = 0
	}

	// handle redirects
	if uow.Message.Response.StatusCode >= 300 && uow.Message.Response.StatusCode <= 399 {
		if uow.Message.Response.Request == nil {
			uow.Message.Response.Request = uow.Message.Request
		}

		absRedirect := GetRedirectLocation(uow.Message.Response)

		if uow.Options.Redirection.PreventCrossOriginRedirects && IsCrossOrigin(uow.Message.Request.URL.String(), absRedirect) {
			return
		}

		if uow.Options.Redirection.PreventCrossSiteRedirects && IsCrossSite(uow.Message.Request.URL.String(), absRedirect) {
			return
		}

		uow.Options.Redirection.currentDepth++
		if uow.Options.Redirection.currentDepth > uow.Options.Redirection.MaxRedirects {
			return
		}

		if !uow.Options.Redirection.FollowRedirects {
			return
		}

		redirectedReq := uow.Message.Request.Clone(c.context)
		uow.Options.CacheBusting.Clear(redirectedReq)

		absRedirectUrl, _ := url.Parse(absRedirect)
		redirectedReq.Host = absRedirectUrl.Host
		redirectedReq.URL, _ = url.Parse(absRedirect)

		newMsg := c.SendWithOptions(redirectedReq, uow.Options)
		c.ThreadPool.lockedThreads <- true
		<-newMsg.Resolved
		<-c.ThreadPool.lockedThreads

		c.MessageLog = append(c.MessageLog, newMsg)
		
		tmpMsg := MessageDuplex{
			Request: uow.Message.Request,
			Response: uow.Message.Response,
			TransportError: uow.Message.TransportError,
			Duration: uow.Message.Duration,
			Prev: uow.Message.Prev,
		}

		uow.Message.Request = newMsg.Request
		uow.Message.Response = newMsg.Response
		uow.Message.TransportError = newMsg.TransportError
		uow.Message.Duration = newMsg.Duration
		uow.Message.Prev = &tmpMsg

		return
	}

	// handle rate-limitting
	if uow.Message.Response.StatusCode == 429 || uow.Message.Response.StatusCode == 529 {
		if uow.Options.Performance.ReplayRateLimitted {
			replayReq := uow.Message.Request.Clone(c.context)
			uow.Message = c.SendWithOptions(replayReq, uow.Options)
		}
	}
}

func (c *HttpClient) calculate429Percentage() uint8 {
	if !c.Options.Performance.AutoRateThrottle || len(c.MessageLog) == 0 {
		return 0
	}
	
	idx := len(c.MessageLog) - 100
	if idx < 0 {
		idx = 0
	}
	
	rateLimitedRequests := c.MessageLog[idx:].Search(func(msg *MessageDuplex) bool {
		return msg.Response != nil && msg.Response.StatusCode == 429
	})
	
	return uint8(float32(len(rateLimitedRequests)) / float32(len(c.MessageLog[idx:])) * 100)
}
