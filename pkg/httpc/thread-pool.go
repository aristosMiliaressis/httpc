package httpc

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aristosMiliaressis/httpc/internal/rate"
	"github.com/projectdiscovery/gologger"
)

type PendingRequest struct {
	RawRequest string
	Request    *MessageDuplex
	Options    ClientOptions
}
type RequestQueue chan PendingRequest

type ThreadPool struct {
	threadCount atomic.Int64
	Rate        *rate.RateThrottle
	context     context.Context

	requestPriorityQueues map[Priority]RequestQueue
	requestQueueMutex     sync.RWMutex

	sendRequestCallback func(uow PendingRequest)
}

func NewThreadPool(callback func(uow PendingRequest), context context.Context, rps int) *ThreadPool {
	return &ThreadPool{
		context:               context,
		sendRequestCallback:   callback,
		Rate:                  rate.NewRateThrottle(rps),
		requestPriorityQueues: make(map[Priority]RequestQueue),
	}
}

// TODO: look into https://www.openmymind.net/Leaking-Goroutines/
// https://medium.com/code-chasm/go-concurrency-pattern-worker-pool-a437117025b1
func (tp *ThreadPool) Run() {

	// temporary solution:
	// one thread should be able to send and receive a message every second at least
	maxThreads := tp.Rate.RPS

	for i := 1; true; i++ {

		gologger.Debug().Msgf("threads: %d, desiredRate: %d currentRate: %d\n",
			int(tp.threadCount.Load()), tp.Rate.RPS, tp.Rate.CurrentRate())

		if tp.Rate.CurrentRate() < int64(tp.Rate.RPS) && int(tp.threadCount.Load()) < maxThreads {

			tp.threadCount.Add(1)

			go func(workerID int) {
				for {
					uow := tp.getNextPrioritizedRequest()

					<-tp.Rate.RateLimiter.C
					tp.sendRequestCallback(uow)
					tp.Rate.Tick(time.Now())

					if tp.Rate.CurrentRate() > int64(tp.Rate.RPS) && int(tp.threadCount.Load()) > 1 {
						tp.threadCount.Add(-1)
						return
					}
				}
			}(i)
		}

		<-time.After(time.Millisecond * 500)
	}
}

func (tp *ThreadPool) getNextPrioritizedRequest() PendingRequest {

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
