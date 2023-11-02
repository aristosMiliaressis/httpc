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

func (tp *ThreadPool) Run() {

	for i := 1; true; i++ {

		gologger.Debug().Msgf("threads: %d, desiredRate: %d currentRate: %d\n",
			int(tp.threadCount.Load()), tp.Rate.RPS, tp.Rate.CurrentRate())

		if tp.Rate.CurrentRate() < int64(tp.Rate.RPS) && tp.getPendingCount() > 0 {

			tp.threadCount.Add(1)

			go func(workerID int) {
				for {
					uow := tp.getNextPrioritizedRequest()

					tp.sendRequestCallback(uow)
					tp.Rate.Tick(time.Now())

					if tp.Rate.CurrentRate() > int64(tp.Rate.RPS) || tp.getPendingCount() == 0 {
						tp.threadCount.Add(-1)
						return
					}
				}
			}(i)
		}

		<-time.After(time.Millisecond * 500)
		<-tp.Rate.RateLimiter.C
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

func (tp *ThreadPool) getPendingCount() int {
	sum := 0

	tp.requestQueueMutex.RLock()
	for p := range tp.requestPriorityQueues {
		sum += len(tp.requestPriorityQueues[p])
	}
	tp.requestQueueMutex.RUnlock()

	return sum
}
