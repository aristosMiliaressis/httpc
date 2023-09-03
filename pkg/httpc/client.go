package httpc

import (
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
	errorLog        map[string]int
	errorMutex      sync.Mutex
	apiGateways     map[string]*iprotate.ApiEndpoint
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

	os.Setenv("GODEBUG", "http2client=0")
	return http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       time.Duration(time.Duration(opts.Timeout) * time.Second),
		Transport: &http.Transport{
			Proxy:               proxyURL,
			ForceAttemptHTTP2:   false,
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

	if req.Method == "CONNECT" {
		return c.ConnectRequest(req, opts)
	}

	evt := HttpEvent{
		Request: req.Clone(c.context),
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

		if evt.IsRedirectLoop() {
			evt.RedirectionLoop = true
			return evt
		}

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

	if evt.TransportError != NoError || evt.Response.StatusCode >= 400 {
		c.totalErrors += 1
		c.errorLog["GENERAL"] += 1
	} else {
		c.totalSuccessful += 1
		c.errorLog["GENERAL"] = 0
	}

	if c.errorLog["GENERAL"] > c.Options.ConsecutiveErrorThreshold {
		gologger.Fatal().Msg("Consecutive Error Threshold Exceeded, exiting.")
		os.Exit(1)
	}

	if c.totalSuccessful+c.totalErrors > 40 && (c.totalSuccessful == 0 || 100/((c.totalSuccessful+c.totalErrors)/c.totalErrors) > c.Options.ErrorPercentageThreshold) {
		gologger.Fatal().Msg("Error Percentage Threshold Exceeded, exiting.")
		gologger.Info().Msgf("%d %d %d", c.totalSuccessful, c.totalErrors, c.Options.ErrorPercentageThreshold)
		os.Exit(1)
	}

	if evt.Response.StatusCode == 429 || evt.Response.StatusCode == 503 {
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

func (c *HttpClient) ConnectRequest(req *http.Request, opts *HttpOptions) HttpEvent {
	evt := HttpEvent{}

	c.EventLog = append(c.EventLog, &evt)

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
