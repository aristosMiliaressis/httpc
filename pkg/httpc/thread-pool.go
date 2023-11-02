package httpc

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"context"
	"io"
	"io/ioutil"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/projectdiscovery/gologger"
	"github.com/projectdiscovery/rawhttp"
)

type PendingRequest struct {
	RawRequest string
	Request    *MessageDuplex
	Options    ClientOptions
}
type RequestQueue chan PendingRequest
type Priority int

type ThreadPool struct {
	threadCount atomic.Int64
	Rate        *RateThrottle
	context     context.Context

	requestPriorityQueues map[Priority]RequestQueue
	requestQueueMutex     sync.RWMutex

	sendRequestCallback func(uow PendingRequest)
}

func (c *HttpClient) NewThreadPool() *ThreadPool {
	return &ThreadPool{
		context:               c.context,
		sendRequestCallback:   c.HandleRequest,
		Rate:                  newRateThrottle(c.Options.Performance.RequestsPerSecond),
		requestPriorityQueues: make(map[Priority]RequestQueue),
	}
}

// TODO: look into https://www.openmymind.net/Leaking-Goroutines/
// https://medium.com/code-chasm/go-concurrency-pattern-worker-pool-a437117025b1
func (tp *ThreadPool) Run() {

	maxThreads := 100

	for i := 1; true; i++ {

		gologger.Debug().Msgf("threads: %d, desiredRate: %d currentRate: %d\n",
			int(tp.threadCount.Load()), tp.Rate.rate, tp.Rate.CurrentRate())

		if tp.Rate.CurrentRate() < int64(tp.Rate.rate) && int(tp.threadCount.Load()) < maxThreads {

			tp.threadCount.Add(1)

			go func(workerID int) {
				for {
					uow := tp.GetNextPrioritizedRequest()

					<-tp.Rate.RateLimiter.C
					tp.sendRequestCallback(uow)
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

func (tp *ThreadPool) GetNextPrioritizedRequest() PendingRequest {

	for {
		priorities := []int{}
		tp.requestQueueMutex.RLock()
		for p := range tp.requestPriorityQueues {
			priorities = append(priorities, int(p))
		}
		tp.requestQueueMutex.RUnlock()
		sort.Sort(sort.Reverse(sort.IntSlice(priorities)))

		for _, p := range priorities {
			tp.requestQueueMutex.RLock()
			queue := tp.requestPriorityQueues[Priority(p)]
			tp.requestQueueMutex.RUnlock()
			if len(queue) == 0 {
				continue
			}

			return <-queue
		}
	}
}

func (c *HttpClient) HandleRequest(uow PendingRequest) {
	defer func() { uow.Request.Resolved <- true }()

	var sendErr error
	if uow.RawRequest == "" {
		if uow.Options.Connection.SNI != "" {
			sniClient := createInternalHttpClient(uow.Options)

			uow.Request.Response, sendErr = sniClient.Do(uow.Request.Request)
		} else {
			uow.Request.Response, sendErr = c.client.Do(uow.Request.Request)
		}
	} else {
		rawhttpOptions := rawhttp.DefaultOptions
		rawhttpOptions.AutomaticHostHeader = false
		rawhttpOptions.CustomRawBytes = []byte(uow.RawRequest)
		httpclient := rawhttp.NewClient(rawhttpOptions)
		defer httpclient.Close()

		var err error
		uow.Request.Response, err = httpclient.DoRaw("GET", uow.Request.Request.URL.String(), "", nil, nil)
		if err != nil {
			gologger.Warning().Msgf("Encountered error while sending uow.RawRequest request: %s", err)
		}
	}

	c.MessageLog = append(c.MessageLog, uow.Request)

	if uow.Request.Response == nil && uow.Options.ErrorHandling.RetryTransportFailures {
		if uow.RawRequest == "" {
			retriedMsg := c.SendWithOptions(uow.Request.Request, uow.Options)
			*uow.Request = *retriedMsg
		} else {
			retriedMsg := c.SendRawWithOptions(uow.RawRequest, uow.Request.Request.URL.String(), uow.Options)
			*uow.Request = *retriedMsg
		}
		return
	}

	if uow.RawRequest != "" {
		return
	}

	var dcprsErr error
	if uow.Request.Response != nil && uow.Request.Response.Body != nil {
		var body []byte
		switch uow.Request.Response.Header.Get("Content-Encoding") {
		case "gzip":
			reader, readErr := gzip.NewReader(uow.Request.Response.Body)
			if readErr == nil {
				defer reader.Close()
				body, dcprsErr = ioutil.ReadAll(reader)
			}
		case "br":
			reader := brotli.NewReader(uow.Request.Response.Body)
			body, dcprsErr = ioutil.ReadAll(reader)
		case "deflate":
			reader := flate.NewReader(uow.Request.Response.Body)
			defer reader.Close()
			body, dcprsErr = ioutil.ReadAll(reader)
		default:
			body, dcprsErr = io.ReadAll(uow.Request.Response.Body)
		}

		uow.Request.Response.Body = io.NopCloser(bytes.NewBuffer(body))
	}

	if dcprsErr != nil {
		gologger.Error().Msgf("Error while reading response %s", dcprsErr)
		return
	}

	// handle transport errors
	if sendErr != nil {
		c.handleTransportError(uow.Request, sendErr)
		return
	}

	gologger.Debug().Msgf("%s %s %d\n", uow.Request.Request.URL.String(), uow.Request.Response.Status, uow.Request.Response.ContentLength)

	// Update cookie jar
	if c.Options.MaintainCookieJar && uow.Request.Response.Cookies() != nil {
		for _, cookie := range uow.Request.Response.Cookies() {
			c.AddCookie(cookie.Name, cookie.Value)
		}
	}

	// handle http errors
	if uow.Request.TransportError != NoError || (uow.Request.Response.StatusCode >= 400 && !Contains(safeErrorsList, uow.Request.Response.StatusCode)) {
		c.totalErrors += 1
		c.consecutiveErrors += 1
		c.handleHttpError(uow.Request)
		return
	} else {
		c.totalSuccessful += 1
		c.consecutiveErrors = 0
	}

	// handle redirects
	if uow.Request.Response.StatusCode >= 300 && uow.Request.Response.StatusCode <= 399 {
		absRedirect := GetRedirectLocation(uow.Request.Response)

		uow.Request.CrossOriginRedirect = IsCrossOrigin(uow.Request.Request.URL.String(), absRedirect)
		uow.Request.CrossSiteRedirect = IsCrossSite(uow.Request.Request.URL.String(), absRedirect)

		if uow.Options.Redirection.PreventCrossOriginRedirects && uow.Request.CrossOriginRedirect {
			return
		}

		if uow.Options.Redirection.PreventCrossSiteRedirects && uow.Request.CrossSiteRedirect {
			return
		}

		uow.Options.Redirection.currentDepth++
		if uow.Options.Redirection.currentDepth > uow.Options.Redirection.MaxRedirects {
			uow.Request.MaxRedirectsExheeded = true
			return
		}

		if !uow.Options.Redirection.FollowRedirects {
			return
		}

		redirectedReq := uow.Request.Request.Clone(c.context)
		redirectedReq.Header.Del("Cookie") // TODO: figure out why did i do this??
		uow.Options.CacheBusting.Clear(redirectedReq)

		absRedirectUrl, _ := url.Parse(absRedirect)
		redirectedReq.Host = absRedirectUrl.Host
		redirectedReq.URL, _ = url.Parse(absRedirect)

		newMsg := c.SendWithOptions(redirectedReq, uow.Options)
		newMsg.AddRedirect(uow.Request)
		<-newMsg.Resolved

		c.MessageLog = append(c.MessageLog, newMsg)

		return
	}

	// handle rate-limitting
	if uow.Request.Response.StatusCode == 429 || uow.Request.Response.StatusCode == 529 {
		if uow.Options.Performance.AutoRateThrottle {
			c.ThreadPool.Rate.ChangeRate(c.ThreadPool.Rate.rate - 1)
		}

		if uow.Options.Performance.ReplayRateLimitted {
			replayReq := uow.Request.Request.Clone(c.context)
			uow.Request = c.SendWithOptions(replayReq, uow.Options)
		}

		uow.Request.RateLimited = true
	}
}
