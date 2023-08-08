package httpc

import (
	"net/http"
	"time"
)

type InternalCacheKey string

type HttpEvent struct {
	CacheKey InternalCacheKey

	TransportError TransportError
	Duration       time.Duration

	Request  *http.Request
	Response *http.Response

	// Redirect Chain LinkedList
	Prev *HttpEvent

	RedirectionLoop      bool
	MaxRedirectsExheeded bool
	CrossOriginRedirect  bool
	CrossSiteRedirect    bool

	RateLimited bool
}

func (e HttpEvent) RedirectDepth() int {
	depth := 0
	for {
		if e.Prev == nil {
			return depth
		}

		e = *e.Prev
		depth++
	}
}

func (e HttpEvent) IsRedirectLoop() bool {

	original := e.Request.URL.String()
	new := ToAbsolute(original, e.Response.Header["Location"][0])

	return original == new
}
