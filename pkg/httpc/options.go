package httpc

type HttpOptions struct {
	SimulateBrowserRequests     bool
	RandomizeUserAgent          bool
	DefaultHeaders              map[string]string
	ProxyUrl                    string
	ReqsPerSecond               int
	Timeout                     int
	MaintainCookieJar           bool
	FollowRedirects             bool
	MaxRedirects                int
	PreventCrossSiteRedirects   bool
	PreventCrossOriginRedirects bool
	Delay                       Range
	AutoRateThrottle            bool
	ReplayRateLimitted          bool
	CacheBusting                CacheBustingOptions
	ForceAttemptHTTP1           bool
	ForceAttemptHTTP2           bool
	SNI                         string
	IpBanDetectionThreshold     int
	IpRotateOnIpBan             bool
	currentDepth                int
	ErrorPercentageThreshold    int
	ConsecutiveErrorThreshold   int
	RetryCount                  int
}

var DefaultOptions = HttpOptions{
	SimulateBrowserRequests:   true,
	DefaultHeaders:            map[string]string{},
	Timeout:                   10,
	ReqsPerSecond:             10,
	MaintainCookieJar:         true,
	FollowRedirects:           true,
	PreventCrossSiteRedirects: true,
	MaxRedirects:              20,
	Delay:                     Range{Min: 0, Max: 0},
	AutoRateThrottle:          true,
	ReplayRateLimitted:        true,
	IpBanDetectionThreshold:   4,
	ErrorPercentageThreshold:  10,
	ConsecutiveErrorThreshold: 50,
	RetryCount:                1,
}

type Range struct {
	Min float64
	Max float64
}
