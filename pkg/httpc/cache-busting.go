package httpc

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"
)

var DefaultCacheBusterParam = "cacheBuster"

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

func (opts CacheBustingOptions) getCacheBuster() string {
	if opts.StaticCacheBuster == "" {
		return RandomString(12)
	}

	return opts.StaticCacheBuster
}

func (opts CacheBustingOptions) Apply(req *http.Request) {

	if opts.QueryParam != "" {
		param := req.URL.Query().Get(opts.QueryParam)
		// if param already exists, dont replace it
		if param == "" {
			req.URL.RawQuery = fmt.Sprintf("%s&%s=%s", req.URL.RawQuery, opts.QueryParam, opts.getCacheBuster())
		}
	}

	if opts.Query {
		param := req.URL.Query().Get(DefaultCacheBusterParam)
		// if param already exists, dont replace it
		if param == "" {
			req.URL.RawQuery = fmt.Sprintf("%s&%s=%s", req.URL.RawQuery, DefaultCacheBusterParam, opts.getCacheBuster())
		}
	}

	if opts.Cookie {
		if cookie, ok := req.Header["Cookie"]; !ok && cookie == nil {
			req.Header.Add("Cookie", opts.getCacheBuster()+"=1")
		} else {
			req.Header["Cookie"][0] = req.Header["Cookie"][0] + "; " + opts.getCacheBuster() + "=1"
		}
	}

	if opts.Accept {
		if accept, ok := req.Header["Accept"]; !ok && accept == nil {
			req.Header.Add("Accept", "*/*, text/"+opts.getCacheBuster())
		} else {
			req.Header["Accept"][0] = req.Header["Accept"][0] + ", text/" + opts.getCacheBuster()
		}
	}

	if opts.AcceptEncoding {
		if enc, ok := req.Header["Accept-Encoding"]; !ok && enc == nil {
			req.Header.Add("Accept-Encoding", "*, "+opts.getCacheBuster())
		} else {
			req.Header["Accept-Encoding"][0] = req.Header["Accept-Encoding"][0] + ", " + opts.getCacheBuster()
		}
	}

	if opts.AcceptLanguage {
		if lang, ok := req.Header["Accept-Language"]; !ok && lang == nil {
			req.Header.Add("Accept-Language", "*, "+opts.getCacheBuster())
		} else {
			req.Header["Accept-Language"][0] = req.Header["Accept-Language"][0] + ", " + opts.getCacheBuster()
		}
	}

	if opts.Origin {
		req.Header.Set("Origin", req.URL.Scheme+"://"+opts.getCacheBuster()+"."+req.URL.Host)
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

func (opts CacheBustingOptions) Clear(req *http.Request) {

	if opts.QueryParam != "" {
		q := req.URL.Query()
		q.Del(opts.QueryParam)
		req.URL.RawQuery = q.Encode()
	}

	if opts.Query {
		q := req.URL.Query()
		q.Del(DefaultCacheBusterParam)
		req.URL.RawQuery = q.Encode()
	}

	if opts.Cookie {
		req.Header.Del("Cookie")
	}

	if opts.Accept {
		req.Header.Del("Accept")
	}

	if opts.AcceptEncoding {
		req.Header.Del("Accept-Encoding")
	}

	if opts.AcceptLanguage {
		req.Header.Del("Accept-Language")
	}

	if opts.Origin {
		req.Header.Del("Accept-Origin")
	}

	if opts.Port {
		req.Host = req.URL.Hostname()
	}
}

func RandomString(length int) string {
	var chars = []rune("abcdefghijklmnopqrstuvwxyz")
	s := make([]rune, length)
	for i := range s {
		s[i] = chars[rand.Intn(len(chars))]
	}
	return string(s)
}
