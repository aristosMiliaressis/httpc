package httpc

import (
	"math/rand"
	"net/http"
)

type CacheBustingOptions struct {
	QueryCacheBuster bool
	QueryParam       string
	Origin           bool
	Accept           bool
	Cookie           bool
	AcceptEncoding   bool
	AcceptLanguage   bool
}

var SafeCacheBusting = CacheBustingOptions{
	QueryCacheBuster: true,
	QueryParam:       "cacheBuster",
}

var AggressiveCacheBusting = CacheBustingOptions{
	QueryCacheBuster: true,
	QueryParam:       "cacheBuster",
	Cookie:           true,
	Accept:           true,
	AcceptEncoding:   true,
	AcceptLanguage:   true,
}

func (opts CacheBustingOptions) apply(req *http.Request) {
	if opts.QueryCacheBuster {
		param := req.URL.Query().Get(opts.QueryParam)
		// if param already exists, dont replace it
		if param == "" {
			req.URL.Query().Add(opts.QueryParam, RandomString(12))
		}
	}

	if opts.Cookie {
		if cookie, ok := req.Header["Cookie"]; !ok && cookie[0] == "" {
			req.Header["Cookie"][0] = RandomString(7) + "=1"
		} else {
			req.Header["Cookie"][0] = req.Header["Cookie"][0] + "; " + RandomString(7) + "=1"
		}
	}

	if opts.Accept {
		if accept, ok := req.Header["Accept"]; !ok && accept[0] == "" {
			req.Header["Accept"][0] = "*/*, text/" + RandomString(7)
		} else {
			req.Header["Accept"][0] = req.Header["Accept"][0] + ", text/" + RandomString(7)
		}
	}

	if opts.AcceptEncoding {
		if enc, ok := req.Header["Accept-Encoding"]; !ok && enc[0] == "" {
			req.Header["Accept-Encoding"][0] = "*, " + RandomString(7)
		} else {
			req.Header["Accept-Encoding"][0] = req.Header["Accept-Encoding"][0] + ", " + RandomString(7)
		}
	}

	if opts.AcceptLanguage {
		if lang, ok := req.Header["Accept-Language"]; !ok && lang[0] == "" {
			req.Header["Accept-Language"][0] = "*, " + RandomString(7)
		} else {
			req.Header["Accept-Language"][0] = req.Header["Accept-Language"][0] + ", " + RandomString(7)
		}
	}

	if opts.Origin {
		req.Header["Origin"][0] = req.URL.Scheme + "://" + RandomString(7) + "." + req.URL.Host
	}
}

var chars = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandomString(length int) string {
	s := make([]rune, length)
	for i := range s {
		s[i] = chars[rand.Intn(len(chars))]
	}
	return string(s)
}
