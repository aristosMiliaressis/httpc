package httpc

import (
	"net/url"
	"os"

	"github.com/projectdiscovery/gologger"
)

type ThreadPool struct {
	maxThreads int

	queuedRequestC chan struct {
		req  *MessageDuplex
		opts HttpOptions
	}
	SendRequestCallback func(*MessageDuplex, HttpOptions)
}

type RequestQueue struct {
	Requests []MessageDuplex
}

func (tp *ThreadPool) Run() {
	for i := 1; i <= tp.maxThreads; i++ {
		go func(workerID int) {
			for uow := range tp.queuedRequestC {
				tp.SendRequestCallback(uow.req, uow.opts)
			}
		}(i)
	}
}

func (c *HttpClient) HandleRequest(msg *MessageDuplex, opts HttpOptions) {
	defer func() { msg.Resolved <- true }()

	var err error
	if opts.SNI != "" {
		sniClient := createInternalHttpClient(opts)

		msg.Response, err = sniClient.Do(msg.Request)
	} else {
		msg.Response, err = c.client.Do(msg.Request)
	}
	//

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

		if opts.PreventCrossOriginRedirects && msg.CrossOriginRedirect {
			return
		}

		if opts.PreventCrossSiteRedirects && msg.CrossSiteRedirect {
			return
		}

		// if evt.IsRedirectLoop() {
		// 	evt.RedirectionLoop = true
		// 	return evt
		// }

		opts.currentDepth++
		if opts.currentDepth > opts.MaxRedirects {
			msg.MaxRedirectsExheeded = true
			return
		}

		if !opts.FollowRedirects {
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

	if msg.Response.StatusCode == 429 {
		if opts.AutoRateThrottle {
			opts.Delay.Max += 0.1
			opts.Delay.Min = opts.Delay.Max - 0.1
		}

		if opts.ReplayRateLimitted {
			replayReq := msg.Request.Clone(c.context)
			msg = c.SendWithOptions(replayReq, opts)
		}

		msg.RateLimited = true
	}

	c.MessageLog = append(c.MessageLog, msg)
}
