package httpc

import (
	"math/rand"
	"net/http"
)

var DefaultCacheBusterParam = "cacheBuster"

type CacheBustingOptions struct {
	QueryCacheBuster bool
	Origin           bool
	Accept           bool
	Cookie           bool
	AcceptEncoding   bool
	AcceptLanguage   bool
}

var SafeCacheBusting = CacheBustingOptions{
	QueryCacheBuster: true,
}

var AggressiveCacheBusting = CacheBustingOptions{
	QueryCacheBuster: true,
	Cookie:           true,
	Accept:           true,
	AcceptEncoding:   true,
	AcceptLanguage:   true,
}

func (opts CacheBustingOptions) Apply(req *http.Request) {
	if opts.QueryCacheBuster {
		param := req.URL.Query().Get(DefaultCacheBusterParam)
		// if param already exists, dont replace it
		if param == "" {
			query := req.URL.Query()
			query.Add(DefaultCacheBusterParam, RandomString(12))
			req.URL.RawQuery = query.Encode()
		}
	}

	if opts.Cookie {
		if cookie, ok := req.Header["Cookie"]; !ok && cookie == nil {
			req.Header.Add("Cookie", RandomString(7)+"=1")
		} else {
			req.Header["Cookie"][0] = req.Header["Cookie"][0] + "; " + RandomString(7) + "=1"
		}
	}

	if opts.Accept {
		if accept, ok := req.Header["Accept"]; !ok && accept == nil {
			req.Header.Add("Accept", "*/*, text/"+RandomString(7))
		} else {
			req.Header["Accept"][0] = req.Header["Accept"][0] + ", text/" + RandomString(7)
		}
	}

	if opts.AcceptEncoding {
		if enc, ok := req.Header["Accept-Encoding"]; !ok && enc == nil {
			req.Header.Add("Accept-Encoding", "*, "+RandomString(7))
		} else {
			req.Header["Accept-Encoding"][0] = req.Header["Accept-Encoding"][0] + ", " + RandomString(7)
		}
	}

	if opts.AcceptLanguage {
		if lang, ok := req.Header["Accept-Language"]; !ok && lang == nil {
			req.Header.Add("Accept-Language", "*, "+RandomString(7))
		} else {
			req.Header["Accept-Language"][0] = req.Header["Accept-Language"][0] + ", " + RandomString(7)
		}
	}

	if opts.Origin {
		req.Header.Set("Origin", req.URL.Scheme+"://"+RandomString(7)+"."+req.URL.Host)
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
