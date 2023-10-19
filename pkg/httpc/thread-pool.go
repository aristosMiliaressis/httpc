package httpc

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"sync/atomic"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/projectdiscovery/gologger"
)

type ThreadPool struct {
	threadCount atomic.Int64
	Rate        *RateThrottle
	context     context.Context

	queuedRequestC chan struct {
		req  *MessageDuplex
		opts ClientOptions
	}
	sendRequestCallback func(*MessageDuplex, ClientOptions)
}

func (c *HttpClient) NewThreadPool() *ThreadPool {
	return &ThreadPool{
		context:             c.context,
		sendRequestCallback: c.HandleRequest,
		Rate:                newRateThrottle(c.Options.Performance.RequestsPerSecond),
		queuedRequestC: make(chan struct {
			req  *MessageDuplex
			opts ClientOptions
		}),
	}
}

type RequestQueue struct {
	Requests []MessageDuplex
}

// TODO: look into https://www.openmymind.net/Leaking-Goroutines/
// https://medium.com/code-chasm/go-concurrency-pattern-worker-pool-a437117025b1
func (tp *ThreadPool) Run() {

	maxThreads := 100

	for i := 1; true; i++ {

		if tp.Rate.CurrentRate() < int64(tp.Rate.rate) && int(tp.threadCount.Load()) < maxThreads {

			tp.threadCount.Add(1)

			go func(workerID int) {
				for uow := range tp.queuedRequestC {

					<-tp.Rate.RateLimiter.C
					tp.sendRequestCallback(uow.req, uow.opts)
					tp.Rate.Tick(time.Now())

					if tp.Rate.CurrentRate() > int64(tp.Rate.rate) && int(tp.threadCount.Load()) > 1 {
						tp.threadCount.Add(-1)
						return
					}
				}
			}(i)
		}

		<-time.After(time.Millisecond * 500)
	}
}

func (c *HttpClient) HandleRequest(msg *MessageDuplex, opts ClientOptions) {
	defer func() { msg.Resolved <- true }()

	var err error
	if opts.Connection.SNI != "" {
		sniClient := createInternalHttpClient(opts)

		msg.Response, err = sniClient.Do(msg.Request)
	} else {
		msg.Response, err = c.client.Do(msg.Request)
	}

	if msg.Response != nil && msg.Response.Body != nil {
		var body []byte
		switch msg.Response.Header.Get("Content-Encoding") {
		case "gzip":
			reader, err := gzip.NewReader(msg.Response.Body)
			defer reader.Close()
			if err == nil {
				body, err = ioutil.ReadAll(reader)
			}
		case "br":
			reader := brotli.NewReader(msg.Response.Body)
			body, err = ioutil.ReadAll(reader)
		case "deflate":
			reader := flate.NewReader(msg.Response.Body)
			body, err = ioutil.ReadAll(reader)
			defer reader.Close()
		default:
			body, err = io.ReadAll(msg.Response.Body)
		}
		if err != nil {
			gologger.Error().Msgf("Error while reading response %s", err)
			return
		}

		msg.Response.Body = io.NopCloser(bytes.NewBuffer(body))
	}

	c.MessageLog = append(c.MessageLog, msg)

	if err != nil {
		c.handleError(msg, err)
		return
	}

	gologger.Debug().Msgf("%s %s %d\n", msg.Request.URL.String(), msg.Response.Status, msg.Response.ContentLength)

	if c.Options.MaintainCookieJar && msg.Response.Cookies() != nil {
		for _, cookie := range msg.Response.Cookies() {
			c.AddCookie(cookie.Name, cookie.Value)
		}
	}

	if msg.Response.StatusCode >= 300 && msg.Response.StatusCode <= 399 {
		absRedirect := GetRedirectLocation(msg.Response)

		msg.CrossOriginRedirect = IsCrossOrigin(msg.Request.URL.String(), absRedirect)
		msg.CrossSiteRedirect = IsCrossSite(msg.Request.URL.String(), absRedirect)

		if opts.Redirection.PreventCrossOriginRedirects && msg.CrossOriginRedirect {
			return
		}

		if opts.Redirection.PreventCrossSiteRedirects && msg.CrossSiteRedirect {
			return
		}

		opts.Redirection.currentDepth++
		if opts.Redirection.currentDepth > opts.Redirection.MaxRedirects {
			msg.MaxRedirectsExheeded = true
			return
		}

		if !opts.Redirection.FollowRedirects {
			return
		}

		redirectedReq := msg.Request.Clone(c.context)
		redirectedReq.Header.Del("Cookie") // TODO: test this <-----
		opts.CacheBusting.Clear(redirectedReq)

		absRedirectUrl, _ := url.Parse(absRedirect)
		redirectedReq.Host = absRedirectUrl.Host
		redirectedReq.URL, _ = url.Parse(absRedirect)

		newMsg := c.SendWithOptions(redirectedReq, opts)
		newMsg.AddRedirect(msg)
		<-newMsg.Resolved

		c.MessageLog = append(c.MessageLog, newMsg)

		return
	}

	c.errorMutex.Lock()
	if msg.TransportError != NoError || (msg.Response.StatusCode >= 400 && !Contains(safeErrorsList, msg.Response.StatusCode)) {
		c.totalErrors += 1
		c.errorLog["GENERAL"] += 1
	} else {
		c.totalSuccessful += 1
		c.errorLog["GENERAL"] = 0
	}

	if c.Options.ErrorHandling.ConsecutiveErrorThreshold != 0 &&
		c.errorLog["GENERAL"] > c.Options.ErrorHandling.ConsecutiveErrorThreshold {
		gologger.Fatal().Msgf("Exceeded %d consecutive errors threshold, exiting.", c.Options.ErrorHandling.ConsecutiveErrorThreshold)
		os.Exit(1)
	}
	c.errorMutex.Unlock()

	if c.Options.ErrorHandling.ErrorPercentageThreshold != 0 &&
		c.totalSuccessful+c.totalErrors > 40 &&
		(c.totalSuccessful == 0 || int(100.0/(float64((c.totalSuccessful+c.totalErrors))/float64(c.totalErrors))) > c.Options.ErrorHandling.ErrorPercentageThreshold) {
		gologger.Fatal().Msgf("%d errors out of %d requests exceeded %d%% error threshold, exiting.", c.totalErrors, c.totalSuccessful+c.totalErrors, c.Options.ErrorHandling.ErrorPercentageThreshold)
		os.Exit(1)
	}

	if msg.Response.StatusCode == 429 || msg.Response.StatusCode == 529 {
		if opts.Performance.AutoRateThrottle {
			c.ThreadPool.Rate.ChangeRate(c.ThreadPool.Rate.rate - 1)
		}

		if opts.Performance.ReplayRateLimitted {
			replayReq := msg.Request.Clone(c.context)
			msg = c.SendWithOptions(replayReq, opts)
		}

		msg.RateLimited = true
	}

	c.MessageLog = append(c.MessageLog, msg)
}
