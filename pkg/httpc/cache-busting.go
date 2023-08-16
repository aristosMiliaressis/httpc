package httpc

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"
)

var DefaultCacheBusterParam = "cacheBuster"

type CacheBustingOptions struct {
	Query          bool
	Hostname       bool
	Port           bool
	Origin         bool
	Accept         bool
	Cookie         bool
	AcceptEncoding bool
	AcceptLanguage bool
}

var SafeCacheBusting = CacheBustingOptions{
	Query: true,
}

var AggressiveCacheBusting = CacheBustingOptions{
	Query:          true,
	Cookie:         true,
	Accept:         true,
	AcceptEncoding: true,
	AcceptLanguage: true,
	Hostname:       false,
	Port:           false,
}

func (opts CacheBustingOptions) Apply(req *http.Request) {
	if opts.Query {
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

	if opts.Port {
		port := 0
		for {
			s1 := rand.NewSource(time.Now().UnixNano())
			r1 := rand.New(s1)
			port = r1.Intn(65535)
			if port != 80 && port != 443 {
				break
			}
		}
		req.Host = fmt.Sprintf("%s:%d", req.URL.Hostname(), port)
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
