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
	"github.com/projectdiscovery/gologger/levels"
	"github.com/projectdiscovery/rawhttp"
)

type HttpClient struct {
	context         context.Context
	cancel          context.CancelFunc
	client          http.Client
	Options         HttpOptions
	Rate            *RateThrottle
	EventLog        EventLog
	errorLog        map[string]int // make concurrent
	errorMutex      sync.Mutex
	apiGateways     map[string]*iprotate.ApiEndpoint // make concurrent
	cookieJar       map[string]string
	cookieJarMutex  sync.RWMutex
	totalErrors     int
	totalSuccessful int
}

func (c *HttpClient) GetCookieJar() map[string]string {
	c.cookieJarMutex.RLock()
	defer c.cookieJarMutex.RUnlock()

	return c.cookieJar
}

func NewHttpClient(opts HttpOptions, ctx context.Context) HttpClient {
	ctx, cancel := context.WithCancel(ctx)

	return HttpClient{
		context:   ctx,
		cancel:    cancel,
		Options:   opts,
		Rate:      newRateThrottle(0),
		client:    createInternalHttpClient(opts),
		errorLog:  map[string]int{},
		cookieJar: map[string]string{},
	}
}

func createInternalHttpClient(opts HttpOptions) http.Client {
	proxyURL := http.ProxyFromEnvironment
	if len(opts.ProxyUrl) > 0 {
		pu, err := url.Parse(opts.ProxyUrl)
		if err == nil {
			proxyURL = http.ProxyURL(pu)
		}
	}

	gologger.DefaultLogger.SetMaxLevel(levels.LevelVerbose)
	if !opts.DebugLogging {
		gologger.DefaultLogger.SetMaxLevel(levels.LevelWarning)
	}

	if opts.ForceAttemptHTTP1 {
		os.Setenv("GODEBUG", "http2client=0")
	}

	return http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       time.Duration(time.Duration(opts.Timeout) * time.Second),
		Transport: &http.Transport{
			Proxy:               proxyURL,
			ForceAttemptHTTP2:   opts.ForceAttemptHTTP2,
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

func (c *HttpClient) Send(req *http.Request) HttpEvent {
	return c.SendWithOptions(req, &c.Options)
}

func (c *HttpClient) SendWithOptions(req *http.Request, opts *HttpOptions) HttpEvent {

	c.sleepIfNeeded(opts.Delay)

	evt := HttpEvent{
		Request: req.Clone(c.context),
	}

	if opts.ForceAttemptHTTP2 {
		evt.Request.Header.Del("Connection")
		evt.Request.Header.Del("Upgrade")
		evt.Request.Header.Del("Transfer-Encoding")
	}

	if evt.Request.Header["User-Agent"] == nil {
		evt.Request.Header.Add("User-Agent", opts.DefaultUserAgent)
	}

	for k, v := range c.Options.DefaultHeaders {
		evt.Request.Header.Set(k, v)
	}

	for k, v := range req.Header {
		evt.Request.Header.Set(k, v[0])
	}

	c.cookieJarMutex.RLock()
	for k, v := range c.cookieJar {
		if ContainsCookie(evt.Request, k) {
			continue
		}
		evt.Request.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	c.cookieJarMutex.RUnlock()

	opts.CacheBusting.Apply(evt.Request)

	var start time.Time
	trace := &httptrace.ClientTrace{
		WroteRequest: func(_ httptrace.WroteRequestInfo) {
			// begin the timer after the request is fully written
			start = time.Now()
		},
		GotFirstResponseByte: func() {
			// record when the first byte of the response was received
			evt.Duration = time.Since(start)
		},
	}

	evt.Request = evt.Request.WithContext(httptrace.WithClientTrace(c.context, trace))

	var err error
	if opts.SNI != "" {
		sniClient := createInternalHttpClient(*opts)

		evt.Response, err = sniClient.Do(evt.Request)
	} else {
		evt.Response, err = c.client.Do(evt.Request)
	}

	c.EventLog = append(c.EventLog, &evt)

	if err != nil {
		return c.handleError(evt, err)
	}

	gologger.Debug().Msgf("%s %s %d\n", evt.Request.URL.String(), evt.Response.Status, evt.Response.ContentLength)

	if c.Options.MaintainCookieJar && evt.Response.Cookies() != nil {
		for _, cookie := range evt.Response.Cookies() {
			c.cookieJarMutex.Lock()
			if c.cookieJar[cookie.Name] != cookie.Value {
				c.cookieJar[cookie.Name] = cookie.Value
			}
			c.cookieJarMutex.Unlock()
		}
	}

	if evt.Response.StatusCode >= 300 && evt.Response.StatusCode <= 399 {
		absRedirect := GetRedirectLocation(evt.Response)

		evt.CrossOriginRedirect = IsCrossOrigin(evt.Request.URL.String(), absRedirect)
		evt.CrossSiteRedirect = IsCrossSite(evt.Request.URL.String(), absRedirect)

		if opts.PreventCrossOriginRedirects && evt.CrossOriginRedirect {
			return evt
		}

		if opts.PreventCrossSiteRedirects && evt.CrossSiteRedirect {
			return evt
		}

		// if evt.IsRedirectLoop() {
		// 	evt.RedirectionLoop = true
		// 	return evt
		// }

		opts.currentDepth++
		if opts.currentDepth > opts.MaxRedirects {
			evt.MaxRedirectsExheeded = true
			return evt
		}

		if !opts.FollowRedirects {
			return evt
		}

		redirectedReq := req.Clone(c.context)
		absRedirectUrl, _ := url.Parse(absRedirect)
		redirectedReq.Host = absRedirectUrl.Host
		redirectedReq.URL, _ = url.Parse(absRedirect)

		newEvt := c.SendWithOptions(redirectedReq, opts)
		newEvt.AddRedirect(&evt)

		c.EventLog = append(c.EventLog, &newEvt)

		return newEvt
	}

	c.errorMutex.Lock()
	if evt.TransportError != NoError || evt.Response.StatusCode >= 400 {
		c.totalErrors += 1
		c.errorLog["GENERAL"] += 1
	} else {
		c.totalSuccessful += 1
		c.errorLog["GENERAL"] = 0
	}

	if c.Options.ConsecutiveErrorThreshold != 0 &&
		c.errorLog["GENERAL"] > c.Options.ConsecutiveErrorThreshold {
		gologger.Fatal().Msgf("Exceeded %d consecutive errors threshold, exiting.", c.Options.ConsecutiveErrorThreshold)
		os.Exit(1)
	}
	c.errorMutex.Unlock()

	if c.Options.ErrorPercentageThreshold != 0 &&
		c.totalSuccessful+c.totalErrors > 40 &&
		(c.totalSuccessful == 0 || int(100.0/(float64((c.totalSuccessful+c.totalErrors))/float64(c.totalErrors))) > c.Options.ErrorPercentageThreshold) {
		gologger.Fatal().Msgf("%d errors out of %d requests exceeded %d%% error threshold, exiting.", c.totalErrors, c.totalSuccessful+c.totalErrors, c.Options.ErrorPercentageThreshold)
		os.Exit(1)
	}

	if evt.Response.StatusCode == 429 {
		if opts.AutoRateThrottle {
			opts.Delay.Max += 0.1
			opts.Delay.Min = opts.Delay.Max - 0.1
		}

		if opts.ReplayRateLimitted {
			replayReq := evt.Request.Clone(c.context)
			evt = c.SendWithOptions(replayReq, opts)
		}

		evt.RateLimited = true
	}

	c.EventLog = append(c.EventLog, &evt)

	return evt
}

func (c *HttpClient) SendRaw(rawreq string, baseUrl string) HttpEvent {
	rawhttpOptions := rawhttp.DefaultOptions
	rawhttpOptions.AutomaticHostHeader = false
	rawhttpOptions.CustomRawBytes = []byte(rawreq)
	httpclient := rawhttp.NewClient(rawhttpOptions)
	defer httpclient.Close()

	var err error
	evt := HttpEvent{}
	evt.Response, err = httpclient.DoRaw("GET", baseUrl, "", nil, nil)
	if err != nil {
		gologger.Warning().Msgf("Encountered error while sending raw request: %s", err)
	}

	c.EventLog = append(c.EventLog, &evt)

	return evt
}

func (c *HttpClient) SendRawWithOptions(rawreq string, baseUrl string, opts *HttpOptions) HttpEvent {
	rawhttpOptions := rawhttp.DefaultOptions
	rawhttpOptions.Timeout = time.Duration(opts.Timeout)
	rawhttpOptions.FollowRedirects = opts.FollowRedirects
	rawhttpOptions.MaxRedirects = opts.MaxRedirects
	rawhttpOptions.SNI = opts.SNI
	rawhttpOptions.AutomaticHostHeader = false
	rawhttpOptions.CustomRawBytes = []byte(rawreq)
	httpclient := rawhttp.NewClient(rawhttpOptions)
	defer httpclient.Close()

	var err error
	evt := HttpEvent{}
	for i := 0; i < opts.RetryCount; i++ {
		evt.Response, err = httpclient.DoRaw("GET", baseUrl, "", nil, nil)
		if err == nil {
			break
		}

		gologger.Warning().Msgf("Encountered error while sending raw request: %s", err)
	}

	c.EventLog = append(c.EventLog, &evt)

	return evt
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

func (c *HttpClient) ConnectRequest(proxyUrl *url.URL, destUrl *url.URL, opts *HttpOptions) HttpEvent {
	evt := HttpEvent{}
	c.EventLog = append(c.EventLog, &evt)

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
		return evt
	}
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: basic aGVsbG86d29ybGQ=\r\n\r\n", destUrl.Host, destUrl.Host)
	br := bufio.NewReader(conn)
	evt.Response, err = http.ReadResponse(br, nil)
	if err != nil {
		// connect check failed, ignore error
		return evt
	}
	// It's safe to discard the bufio.Reader here and return the
	// original TCP conn directly because we only use this for
	// TLS, and in TLS the client speaks first, so we know there's
	// no unbuffered data. But we can double-check.
	if br.Buffered() > 0 {
		gologger.Error().Msgf("unexpected %d bytes of buffered data from CONNECT proxy %q", br.Buffered(), proxyAddr)
	}
	return evt
}

func ContainsCookie(req *http.Request, cookieName string) bool {
	for _, cookie := range req.Cookies() {
		if cookie.Name == cookieName {
			return true
		}
	}
	return false
}
