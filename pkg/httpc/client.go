package httpc

import (
	"context"
	"crypto/tls"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"syscall"
	"time"

	"github.com/projectdiscovery/rawhttp"
)

type HttpClient struct {
	context  context.Context
	cancel   context.CancelFunc
	client   http.Client
	options  HttpOptions
	Rate     *RateThrottle
	EventLog []*HttpEvent
}

func NewHttpClient(opts HttpOptions) HttpClient {
	ctx, cancel := context.WithCancel(context.Background())

	proxyURL := http.ProxyFromEnvironment
	if len(opts.ProxyUrl) > 0 {
		pu, err := url.Parse(opts.ProxyUrl)
		if err == nil {
			proxyURL = http.ProxyURL(pu)
		}
	}

	return HttpClient{
		context: ctx,
		cancel:  cancel,
		options: opts,
		Rate:    newRateThrottle(0),
		client: http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
			Timeout:       time.Duration(time.Duration(opts.Timeout) * time.Second),
			Transport: &http.Transport{
				Proxy:               proxyURL,
				ForceAttemptHTTP2:   false,
				DisableCompression:  true,
				TLSNextProto:        map[string]func(string, *tls.Conn) http.RoundTripper{},
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
					//Renegotiation:      tls.RenegotiateOnceAsClient,
					//ServerName: sni,
				},
			},
		},
	}
}

func (c *HttpClient) Send(req *http.Request) HttpEvent {
	return c.SendWithOptions(req, c.options)
}

func (c *HttpClient) SendWithOptions(req *http.Request, opts HttpOptions) HttpEvent {

	c.sleepIfNeeded(opts.Delay)

	evt := HttpEvent{
		Request: req,
	}

	if evt.Request.Header["User-Agent"] == nil {
		evt.Request.Header.Add("User-Agent", opts.DefaultUserAgent)
	}

	opts.CacheBusting.apply(evt.Request)

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
	evt.Response, err = c.client.Do(evt.Request)
	if err != nil {
		if os.IsTimeout(err) {
			evt.TransportError = Timeout
		} else if errors.Is(err, syscall.ECONNRESET) {
			evt.TransportError = ConnectionReset
		} else {
			// TODO: handle all errors
		}

		return evt
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

		if !opts.FollowRedirects {
			return evt
		}

		if evt.RedirectDepth() > opts.MaxRedirects {
			evt.MaxRedirectsExheeded = true
			return evt
		}

		redirectedReq := evt.Request.Clone(c.context)
		redirectedReq.URL, _ = url.Parse(absRedirect)

		newEvt := c.Send(redirectedReq)
		newEvt.Prev = &evt

		return newEvt
	}

	if evt.Response.StatusCode == 429 {
		if opts.AutoRateThrottle {
			opts.Delay.Max += 0.1
		}

		if opts.ReplayRateLimitted {
			replayReq := evt.Request.Clone(c.context)
			evt = c.Send(replayReq)
		}

		evt.RateLimited = true
	}

	return evt
}

func (c *HttpClient) SendRaw(rawreq string, baseUrl string, scheme string) HttpEvent {
	rawhttpOptions := rawhttp.Options{
		Timeout:                time.Duration(c.options.Timeout * 1000),
		AutomaticHostHeader:    false,
		AutomaticContentLength: false,
		CustomRawBytes:         []byte(rawreq),
		FollowRedirects:        c.options.FollowRedirects,
		MaxRedirects:           c.options.MaxRedirects,
		Proxy:                  c.options.ProxyUrl,
		//SNI
	}
	httpclient := rawhttp.NewClient(&rawhttpOptions)
	defer httpclient.Close()

	var err error
	evt := HttpEvent{}
	evt.Response, err = httpclient.DoRaw("", baseUrl, "/", map[string][]string{}, nil)
	if err != nil {
		// TODO: handle errors
	}

	return evt
}

func (c *HttpClient) sleepIfNeeded(delay Range) {

	sTime := delay.Min + rand.Float64()*(delay.Max-delay.Min)
	sleepDuration := time.Duration(sTime * 1000)

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
