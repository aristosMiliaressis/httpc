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
	Message    *MessageDuplex
	Options    ClientOptions
}
type RequestQueue chan PendingRequest

type ThreadPool struct {
	Rate    *rate.RateThrottle
	context context.Context

	queuePriorityMap   map[Priority]RequestQueue
	queuePriorityMutex sync.RWMutex
	queueBufferSize    int

	processCallback func(uow PendingRequest)
}

func (tp *ThreadPool) NewRequestQueue() RequestQueue {
	return make(RequestQueue, tp.queueBufferSize)
}

func NewThreadPool(callback func(uow PendingRequest), context context.Context, rps int, bufferSize int) *ThreadPool {
	return &ThreadPool{
		context:          context,
		queueBufferSize:  bufferSize,
		processCallback:  callback,
		Rate:             rate.NewRateThrottle(rps),
		queuePriorityMap: make(map[Priority]RequestQueue),
	}
}

func (tp *ThreadPool) Run() {

	var threadCount atomic.Int64
	var threadLimiter bool
	delay := time.Millisecond * 1

	for i := 1; true; i++ {

		threadLimiter = true

		gologger.Debug().Msgf("threads: %d, desiredRate: %d, currentRate: %d, delay: %dms\n",
			int(threadCount.Load()), tp.Rate.RPS, tp.Rate.CurrentRate(), delay.Milliseconds())

		if tp.Rate.CurrentRate() < int64(tp.Rate.RPS) && tp.getPendingCount() > 0 {

			threadCount.Add(1)

			go func(workerID int) {
				for {
					uow := tp.getNextPrioritizedRequest()

					tp.processCallback(uow)
					tp.Rate.Tick(time.Now())

					if threadLimiter && (tp.Rate.CurrentRate() > int64(tp.Rate.RPS) || tp.getPendingCount() == 0) {

						threadLimiter = false

						if int(threadCount.Load()) > 1 {
							threadCount.Add(-1)
							return
						} else {
							delay += time.Millisecond * 100
						}
					}

					<-time.After(delay)
				}
			}(i)
		}

		<-time.After(time.Millisecond * 1000)
	}
}

func (tp *ThreadPool) getNextPrioritizedRequest() PendingRequest {

	for {
		priorities := []int{}
		tp.queuePriorityMutex.RLock()
		for p := range tp.queuePriorityMap {
			priorities = append(priorities, int(p))
		}
		tp.queuePriorityMutex.RUnlock()
		sort.Sort(sort.Reverse(sort.IntSlice(priorities)))

		for _, p := range priorities {
			tp.queuePriorityMutex.RLock()
			queue := tp.queuePriorityMap[Priority(p)]
			tp.queuePriorityMutex.RUnlock()
			if len(queue) == 0 {
				continue
			}

			return <-queue
		}
	}
}

func (tp *ThreadPool) getPendingCount() int {
	sum := 0

	tp.queuePriorityMutex.RLock()
	for p := range tp.queuePriorityMap {
		sum += len(tp.queuePriorityMap[p])
	}
	tp.queuePriorityMutex.RUnlock()

	return sum
}
