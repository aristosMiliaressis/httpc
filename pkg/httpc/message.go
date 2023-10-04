package httpc

import (
	"net/http"
	"time"
)

type MessageDuplex struct {
	TransportError TransportError
	Duration       time.Duration

	Request  *http.Request
	Response *http.Response
	Resolved chan bool

	// Redirect Chain LinkedList
	Prev *MessageDuplex

	RedirectionLoop      bool
	MaxRedirectsExheeded bool
	CrossOriginRedirect  bool
	CrossSiteRedirect    bool

	RateLimited bool
}

func (e MessageDuplex) RedirectDepth() int {
	depth := 0
	for {
		if e.Prev == nil {
			return depth
		}

		e = *e.Prev
		depth++
	}
}

func (e MessageDuplex) IsRedirectLoop() bool {

	originalWithCacheBuster := e.Request.URL.String()

	query := e.Request.URL.Query()
	query.Del(DefaultCacheBusterParam)
	e.Request.URL.RawQuery = query.Encode()

	original := e.Request.URL.String()
	if e.Response == nil || len(e.Response.Header["Location"]) == 0 {
		return false
	}

	new := ToAbsolute(original, e.Response.Header["Location"][0])

	return original == new || originalWithCacheBuster == new
}

func (e *MessageDuplex) AddRedirect(prev *MessageDuplex) {
	current := e

	for {
		if current.Prev == nil {
			current.Prev = prev
			break
		}
		current = current.Prev
	}
}
