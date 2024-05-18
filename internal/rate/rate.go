package rate

import (
	"container/ring"
	"sync"
	"time"
)

type RateThrottle struct {
	RPS int

	RateLimiter *time.Ticker
	rateCounter *ring.Ring
	rateMutex   sync.Mutex
	throttleRate uint64
}

func NewRateThrottle(rate int) *RateThrottle {
	r := &RateThrottle{
		RPS: rate,
	}

	ratemicros := 1000000/r.RPS - 50000/r.RPS
	r.RateLimiter = time.NewTicker(time.Microsecond * time.Duration(ratemicros))
	r.rateCounter = ring.New(r.RPS * 5)
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
	if rate <= 0 {
		return
	}

	r.RPS = rate
	r.RateLimiter.Stop()

	ratemicros := 1000000/r.RPS - 50000/r.RPS

	r.RateLimiter = time.NewTicker(time.Microsecond * time.Duration(ratemicros))
	r.rateCounter = ring.New(rate * 5)
}

func (r *RateThrottle) Stop() {
	r.RPS = 0
	r.RateLimiter.Stop()
	r.rateCounter = ring.New(0)
}

func (r *RateThrottle) SetRatelimitPercentage(percentage uint8) {
	if percentage > 100 {
		panic("Ratelimit percentage above 100 passed, that's a bug")
	}
	r.throttleRate = uint64(float64(r.RPS) / 100 * float64(percentage))
}

func (r *RateThrottle) GetThrottleRate() uint64 {
	return r.throttleRate
}

// rateTick adds a new duration measurement Tick to rate counter
func (r *RateThrottle) Tick(end time.Time) {
	r.rateMutex.Lock()
	defer r.rateMutex.Unlock()
	
	if r.RPS == 0 {
		return
	}

	r.rateCounter = r.rateCounter.Next()
	r.rateCounter.Value = end.UnixMicro()
}
