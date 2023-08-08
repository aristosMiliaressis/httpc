package httpc

import (
	"container/ring"
	"sync"
	"time"
)

type RateThrottle struct {
	RateLimiter    *time.Ticker // ticks on rps based interval?
	rateCounter    *ring.Ring   // Tick() called manually?
	reqPerSec      int
	rateMutex      sync.Mutex
	lastAdjustment time.Time
}

func newRateThrottle(rate int) *RateThrottle {
	r := &RateThrottle{
		reqPerSec:      rate,
		lastAdjustment: time.Now(),
	}

	if r.reqPerSec > 0 {
		ratemicros := 1000000 / r.reqPerSec
		r.RateLimiter = time.NewTicker(time.Microsecond * time.Duration(ratemicros))
		r.rateCounter = ring.New(r.reqPerSec * 5)
		return r
	}

	r.RateLimiter = time.NewTicker(time.Microsecond * 1)
	r.rateCounter = ring.New(5)

	return r
}

// CurrentRate calculates requests/second value from circular list of rate
func (r *RateThrottle) CurrentRate() int64 {
	n := r.rateCounter.Len()
	lowest := int64(0)
	highest := int64(0)
	r.rateCounter.Do(func(r interface{}) {
		switch val := r.(type) {
		case int64:
			if lowest == 0 || val < lowest {
				lowest = val
			}
			if val > highest {
				highest = val
			}
		default:
			// circular list entry was nil, happens when < number_of_threads * 5 responses have been recorded.
			// the total number of entries is less than length of the list
			n -= 1
		}
	})

	earliest := time.UnixMicro(lowest)
	latest := time.UnixMicro(highest)
	elapsed := latest.Sub(earliest)
	if n > 0 && elapsed.Milliseconds() > 1 {
		return int64(1000 * int64(n) / elapsed.Milliseconds())
	}
	return 0
}

func (r *RateThrottle) ChangeRate(rate int) {
	r.reqPerSec = rate
	ratemicros := 1000000 / rate

	r.RateLimiter.Stop()
	r.RateLimiter = time.NewTicker(time.Microsecond * time.Duration(ratemicros))
	r.rateCounter = ring.New(rate * 5)
}

// rateTick adds a new duration measurement tick to rate counter
func (r *RateThrottle) tick(end time.Time) {
	r.rateMutex.Lock()
	defer r.rateMutex.Unlock()

	r.rateCounter = r.rateCounter.Next()
	r.rateCounter.Value = end.UnixMicro()
}
