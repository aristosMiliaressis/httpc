package httpc

import (
	"github.com/projectdiscovery/rawhttp"
	"github.com/aristosMiliaressis/httpc/internal/util"
)

type ClientOptions struct {
	SimulateBrowserRequests bool
	RandomizeUserAgent      bool
	MaintainCookieJar       bool
	DefaultHeaders          map[string]string
	RequestPriority         Priority
	ExcludeCookies          []string

	Connection    ConnectionOptions
	CacheBusting  CacheBustingOptions
	Redirection   RedirectionOptions
	Performance   PerformanceOptions
	ErrorHandling ErrorHandlingOptions
	RawHttp       rawhttp.Options
}

type ConnectionOptions struct {
	ProxyUrl          string
	ForceAttemptHTTP1 bool
	ForceAttemptHTTP2 bool
	DisableKeepAlives bool
	EnableIPRotate    bool
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
	PercentageThreshold      int
	ConsecutiveThreshold     int
	VerifyIPBanIfExheeded    bool
	IpRotateIfExheeded       bool
	ReportErrorsIfExheeded   bool
	RetryTransportFailures   bool
	HandleErrorCodes         []int
	ReverseErrorCodeHandling bool
	AwsProfile               string
}

type CacheBustingOptions struct {
	Query             bool   `json:",omitempty"`
	Hostname          bool   `json:",omitempty"`
	Port              bool   `json:",omitempty"`
	Origin            bool   `json:",omitempty"`
	Accept            bool   `json:",omitempty"`
	Cookie            bool   `json:",omitempty"`
	AcceptEncoding    bool   `json:",omitempty"`
	AcceptLanguage    bool   `json:",omitempty"`
	StaticCacheBuster string `json:"-"`
	QueryParam        string `json:",omitempty"`
	CookieParam       string `json:",omitempty"`
	AddEverywhere     bool   `json:",omitempty"`
}

type Range struct {
	Min float64
	Max float64
}

type Priority int

var DefaultOptions = ClientOptions{
	SimulateBrowserRequests: true,
	MaintainCookieJar:       true,
	RequestPriority:         1,
	DefaultHeaders:          map[string]string{},
	Connection: ConnectionOptions{
		DisableKeepAlives: true,
	},
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
		Delay:              Range{Min: 0, Max: 0.1},
	},
	ErrorHandling: ErrorHandlingOptions{
		PercentageThreshold:    90,
		ConsecutiveThreshold:   0,
		VerifyIPBanIfExheeded:  true,
		ReportErrorsIfExheeded: true,
		HandleErrorCodes:       []int{401, 402, 404, 405, 406, 407, 410, 411, 412, 413, 414, 415, 416, 417, 426, 431, 500, 501},
	},
	RawHttp: rawhttp.Options{
		AutomaticHostHeader: false,
	},
}

func (opts ErrorHandlingOptions) Matches(statusCode int) bool {
	if util.Contains(opts.HandleErrorCodes, statusCode) {
		return opts.ReverseErrorCodeHandling
	}

	return !opts.ReverseErrorCodeHandling
}
