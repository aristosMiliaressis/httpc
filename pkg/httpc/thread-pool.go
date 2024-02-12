package httpc

import (
	"context"
	"fmt"
	"math/rand"
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
	Rate     *rate.RateThrottle
	minDelay float64
	maxDelay float64
	context  context.Context

	queuePriorityMap   map[Priority]RequestQueue
	queuePriorityMutex sync.RWMutex
	queueBufferSize    int

	lockedWorkers chan bool
	processCallback func(uow PendingRequest)
}

func (tp *ThreadPool) NewRequestQueue() RequestQueue {
	return make(RequestQueue, tp.queueBufferSize)
}

func NewThreadPool(callback func(uow PendingRequest), context context.Context, rps int, delay Range, bufferSize int) *ThreadPool {
	return &ThreadPool{
		context:          context,
		queueBufferSize:  bufferSize,
		processCallback:  callback,
		minDelay:         delay.Min,
		maxDelay:         delay.Max,
		lockedWorkers: make(chan bool, bufferSize),
		Rate:             rate.NewRateThrottle(rps),
		queuePriorityMap: make(map[Priority]RequestQueue),
	}
}

func (tp *ThreadPool) Run() {

	var threadCount atomic.Int64
	var threadLimiter bool

	for i := 1; true; i++ {

		threadLimiter = true

		pending := tp.getPendingCount()

		gologger.Debug().Msgf("threads: %d, desiredRate: %d, currentRate: %d, delay: %f-%fs, pending: %d\n",
			int(threadCount.Load()), tp.Rate.RPS, tp.Rate.CurrentRate(), tp.minDelay, tp.maxDelay, pending)

		if tp.Rate.CurrentRate() < int64(tp.Rate.RPS) && tp.getPendingCount() > 0 {

			threadCount.Add(1)

			go func(workerID int) {
				for {
					uow := tp.getNextPrioritizedRequest()

					tp.processCallback(uow)
					tp.Rate.Tick(time.Now())

					if threadLimiter && (tp.Rate.CurrentRate() > int64(tp.Rate.RPS) || tp.getPendingCount() == 0) {

						threadLimiter = false

						if int(threadCount.Load()) - len(tp.lockedWorkers) > 1 {
							threadCount.Add(-1)
							return
						} else {
							tp.minDelay += 0.1
							tp.maxDelay += 0.1
						}
					}

					tp.sleepIfNeeded()
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

func (tp *ThreadPool) sleepIfNeeded() {

	sTime := tp.minDelay + rand.Float64()*(tp.maxDelay-tp.minDelay)
	sleepDuration, _ := time.ParseDuration(fmt.Sprintf("%dms", int(sTime*1000)))

	select {
	case <-tp.context.Done():
	case <-time.After(sleepDuration):
	}
}
