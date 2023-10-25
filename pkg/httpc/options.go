package httpc

type ClientOptions struct {
	SimulateBrowserRequests bool
	RandomizeUserAgent      bool
	MaintainCookieJar       bool
	DefaultHeaders          map[string]string
	RequestPriority         Priority

	Connection    ConnectionOptions
	CacheBusting  CacheBustingOptions
	Redirection   RedirectionOptions
	Performance   PerformanceOptions
	ErrorHandling ErrorHandlingOptions
}

type ConnectionOptions struct {
	ProxyUrl          string
	ForceAttemptHTTP1 bool
	ForceAttemptHTTP2 bool
	SNI               string
}

type RedirectionOptions struct {
	FollowRedirects             bool
	MaxRedirects                int
	PreventCrossSiteRedirects   bool
	PreventCrossOriginRedirects bool
	currentDepth                int
}

type PerformanceOptions struct {
	Timeout            int
	RequestsPerSecond  int
	Delay              Range
	AutoRateThrottle   bool
	ReplayRateLimitted bool
}

type ErrorHandlingOptions struct {
	ErrorPercentageThreshold    int
	ConsecutiveErrorThreshold   int
	IpRotateIfThresholdExheeded bool
	RetryTransportFailures      bool
}

type Range struct {
	Min float64
	Max float64
}

var DefaultOptions = ClientOptions{
	SimulateBrowserRequests: true,
	MaintainCookieJar:       true,
	RequestPriority:         1,
	DefaultHeaders:          map[string]string{},
	Redirection: RedirectionOptions{
		FollowRedirects:           true,
		PreventCrossSiteRedirects: true,
		MaxRedirects:              10,
	},
	Performance: PerformanceOptions{
		Timeout:            10,
		RequestsPerSecond:  10,
		AutoRateThrottle:   true,
		ReplayRateLimitted: true,
		Delay:              Range{Min: 0, Max: 0},
	},
	ErrorHandling: ErrorHandlingOptions{
		ErrorPercentageThreshold:  0,
		ConsecutiveErrorThreshold: 0,
	},
}
