package httpc

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/aristosMiliaressis/go-ip-rotate/pkg/iprotate"
	"github.com/projectdiscovery/gologger"
	"github.com/projectdiscovery/rawhttp"
)

type HttpClient struct {
	context    context.Context
	cancel     context.CancelFunc
	client     http.Client
	Options    HttpOptions
	ThreadPool *ThreadPool

	MessageLog MessageLog

	cookieJar      map[string]string
	cookieJarMutex sync.RWMutex

	errorLog   map[string]int
	errorMutex sync.Mutex

	totalErrors     int
	totalSuccessful int

	apiGateways map[string]*iprotate.ApiEndpoint // make concurrent
}

func NewHttpClient(opts HttpOptions, ctx context.Context) *HttpClient {
	ctx, cancel := context.WithCancel(ctx)

	c := HttpClient{
		context:   ctx,
		cancel:    cancel,
		Options:   opts,
		client:    createInternalHttpClient(opts),
		errorLog:  map[string]int{},
		cookieJar: map[string]string{},
	}

	c.ThreadPool = c.NewThreadPool()
	go c.ThreadPool.Run()

	return &c
}

func (c *HttpClient) Close() {
	close(c.ThreadPool.queuedRequestC)
}

func createInternalHttpClient(opts HttpOptions) http.Client {
	proxyURL := http.ProxyFromEnvironment
	if len(opts.ProxyUrl) > 0 {
		pu, err := url.Parse(opts.ProxyUrl)
		if err == nil {
			proxyURL = http.ProxyURL(pu)
		}
	}

	if opts.ForceAttemptHTTP1 {
		os.Setenv("GODEBUG", "http2client=0")
	}

	return http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       time.Duration(time.Duration(opts.Timeout) * time.Second),
		Transport: &http.Transport{
			Proxy:             proxyURL,
			ForceAttemptHTTP2: opts.ForceAttemptHTTP2,
			//DisableKeepAlives:   true,
			DisableCompression:  true,
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 500,
			MaxConnsPerHost:     500,
			DialContext: (&net.Dialer{
				Timeout: time.Duration(time.Duration(opts.Timeout) * time.Second),
			}).DialContext,
			TLSHandshakeTimeout: time.Duration(time.Duration(opts.Timeout) * time.Second),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				MinVersion:         tls.VersionSSL30,
				Renegotiation:      tls.RenegotiateOnceAsClient,
				ServerName:         opts.SNI,
			},
		},
	}
}

func (c *HttpClient) Send(req *http.Request) *MessageDuplex {
	return c.SendWithOptions(req, c.Options)
}

func (c *HttpClient) SendWithOptions(req *http.Request, opts HttpOptions) *MessageDuplex {

	c.sleepIfNeeded(opts.Delay)

	msg := &MessageDuplex{
		Request:  req.Clone(c.context),
		Resolved: make(chan bool, 1),
	}

	if opts.ForceAttemptHTTP2 {
		msg.Request.Header.Del("Connection")
		msg.Request.Header.Del("Upgrade")
		msg.Request.Header.Del("Transfer-Encoding")
	}

	if msg.Request.Header["User-Agent"] == nil {
		msg.Request.Header.Set("User-Agent", opts.DefaultUserAgent)
	}

	for k, v := range c.Options.DefaultHeaders {
		msg.Request.Header.Set(k, v)
	}

	for k, v := range req.Header {
		msg.Request.Header.Set(k, v[0])
	}

	for k, v := range c.GetCookieJar() {
		if ContainsCookie(msg.Request, k) {
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

	c.ThreadPool.queuedRequestC <- struct {
		req  *MessageDuplex
		opts HttpOptions
	}{msg, opts}

	return msg
}

func (c *HttpClient) SendRaw(rawreq string, baseUrl string) *MessageDuplex {
	rawhttpOptions := rawhttp.DefaultOptions
	rawhttpOptions.AutomaticHostHeader = false
	rawhttpOptions.CustomRawBytes = []byte(rawreq)
	httpclient := rawhttp.NewClient(rawhttpOptions)
	defer httpclient.Close()

	var err error
	msg := MessageDuplex{}
	msg.Response, err = httpclient.DoRaw("GET", baseUrl, "", nil, nil)
	if err != nil {
		gologger.Warning().Msgf("Encountered error while sending raw request: %s", err)
	}

	c.MessageLog = append(c.MessageLog, &msg)

	return &msg
}

func (c *HttpClient) SendRawWithOptions(rawreq string, baseUrl string, opts HttpOptions) *MessageDuplex {
	rawhttpOptions := rawhttp.DefaultOptions
	rawhttpOptions.Timeout = time.Duration(opts.Timeout * int(time.Second))
	rawhttpOptions.FollowRedirects = opts.FollowRedirects
	rawhttpOptions.MaxRedirects = opts.MaxRedirects
	rawhttpOptions.SNI = opts.SNI
	rawhttpOptions.AutomaticHostHeader = false
	rawhttpOptions.CustomRawBytes = []byte(rawreq)
	httpclient := rawhttp.NewClient(rawhttpOptions)
	defer httpclient.Close()

	var err error
	msg := MessageDuplex{}
	for i := 0; i < opts.RetryCount; i++ {
		msg.Response, err = httpclient.DoRaw("GET", baseUrl, "", nil, nil)
		if err == nil {
			break
		}

		gologger.Warning().Msgf("Encountered error while sending raw request: %s", err)
	}

	c.MessageLog = append(c.MessageLog, &msg)

	return &msg
}

func (c *HttpClient) sleepIfNeeded(delay Range) {

	sTime := delay.Min + rand.Float64()*(delay.Max-delay.Min)
	sleepDuration, _ := time.ParseDuration(fmt.Sprintf("%dms", int(sTime*1000)))

	select {
	case <-c.context.Done():
	case <-time.After(sleepDuration):
	}
}

func GetRedirectLocation(resp *http.Response) string {

	requestUrl, _ := url.Parse(resp.Request.URL.String())
	requestUrl.RawQuery = ""

	redirectLocation := ""
	if loc, ok := resp.Header["Location"]; ok {
		if len(loc) > 0 {
			redirectLocation = loc[0]
		}
	}

	return ToAbsolute(resp.Request.URL.String(), redirectLocation)
}

func (c *HttpClient) ConnectRequest(proxyUrl *url.URL, destUrl *url.URL, opts HttpOptions) *MessageDuplex {
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

func Contains[T int | string](s []T, e T) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}
