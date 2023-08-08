package httpc

type HttpOptions struct {
	DefaultUserAgent            string
	ProxyUrl                    string
	Timeout                     int
	FollowRedirects             bool
	MaxRedirects                int
	PreventCrossSiteRedirects   bool
	PreventCrossOriginRedirects bool
	Delay                       Range
	AutoRateThrottle            bool
	ReplayRateLimitted          bool
	CacheBusting                CacheBustingOptions
}

var DefaultOptions = HttpOptions{
	DefaultUserAgent:          "Mozilla/5.0 (iPhone; CPU iPhone OS 16_5_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.5 Mobile/15E148 Safari/604.1",
	Timeout:                   15,
	FollowRedirects:           true,
	PreventCrossSiteRedirects: true,
	MaxRedirects:              20,
	Delay:                     Range{Min: 0, Max: 0},
	AutoRateThrottle:          true,
	ReplayRateLimitted:        true,
}

type Range struct {
	Min float64
	Max float64
}
