package httpc

import (
	"container/ring"
	"sync"
	"time"
)

type RateThrottle struct {
	rate int

	RateLimiter *time.Ticker
	rateCounter *ring.Ring
	rateMutex   sync.Mutex
}

func newRateThrottle(rate int) *RateThrottle {
	r := &RateThrottle{
		rate: rate,
	}

	ratemicros := 1000000/r.rate - 50000/r.rate
	r.RateLimiter = time.NewTicker(time.Microsecond * time.Duration(ratemicros))
	r.rateCounter = ring.New(r.rate * 5)
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
	r.rate = rate
	ratemicros := 1000000/r.rate - 50000/r.rate

	r.RateLimiter.Stop()
	r.RateLimiter = time.NewTicker(time.Microsecond * time.Duration(ratemicros))
	r.rateCounter = ring.New(rate * 5)
}

// rateTick adds a new duration measurement Tick to rate counter
func (r *RateThrottle) Tick(end time.Time) {
	r.rateMutex.Lock()
	defer r.rateMutex.Unlock()

	r.rateCounter = r.rateCounter.Next()
	r.rateCounter.Value = end.UnixMicro()
}
