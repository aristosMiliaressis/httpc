package httpc

import (
	"net/http"
	"time"
)

type HttpEvent struct {
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

	originalWithCacheBuster := e.Request.URL.String()

	query := e.Request.URL.Query()
	query.Del(DefaultCacheBusterParam)
	e.Request.URL.RawQuery = query.Encode()

	original := e.Request.URL.String()
	new := ToAbsolute(original, e.Response.Header["Location"][0])

	return original == new || originalWithCacheBuster == new
}

func (e *HttpEvent) AddRedirect(prev *HttpEvent) {
	current := e

	for {
		if current.Prev == nil {
			current.Prev = prev
			break
		}
		current = current.Prev
	}
}
