package util

import (
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/publicsuffix"
)

func IsCrossOrigin(urlA string, urlB string) bool {
	a, _ := url.Parse(urlA)
	b, _ := url.Parse(urlB)

	return a.Scheme != b.Scheme || a.Host != b.Host
}

func IsCrossSite(urlA string, urlB string) bool {
	a, _ := url.Parse(urlA)
	b, _ := url.Parse(urlB)

	eTLDPlus1A, _ := publicsuffix.EffectiveTLDPlusOne(a.Hostname())
	eTLDPlus1B, _ := publicsuffix.EffectiveTLDPlusOne(b.Hostname())

	return eTLDPlus1A != eTLDPlus1B
}

func ToAbsolute(src string, target string) string {

	srcUrl, _ := url.Parse(src)

	parsedUrl, err := url.Parse(target)

	if target == "" {
		target = srcUrl.String()
	} else if err != nil {
		target = ""
	} else if parsedUrl.IsAbs() || strings.HasPrefix(target, "//") || strings.HasPrefix(target, "\\") {
		// keep absolute urls as is
		_ = target
	} else {
		// concatenate relative urls with base url

		if strings.HasPrefix(target, "/") {
			// root relative
			srcUrl.Path = ""
			srcUrl.RawQuery = ""
			target = srcUrl.String() + target
		} else {
			// current path relative
			if !strings.HasSuffix(srcUrl.String(), "/") {
				target = "/" + target
			}

			srcUrl.RawQuery = ""
			target = srcUrl.String() + target
		}
	}

	return target
}

func GetBaseUrl(url *url.URL) *url.URL {
	baseUrl, _ := url.Parse(url.Scheme + "://" + url.Host)
	return baseUrl
}

func GetRedirectLocation(resp *http.Response) string {

	requestUrl, _ := url.Parse(resp.Request.URL.String())
	requestUrl.RawQuery = ""

	redirectLocation := ""
	if loc, ok := resp.Header["Location"]; ok {
		if len(loc) > 0 {
			redirectLocation = loc[0]
		}
	}

	return ToAbsolute(resp.Request.URL.String(), redirectLocation)
}
