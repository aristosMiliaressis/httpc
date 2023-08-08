package httpc

import (
	"net/url"

	"golang.org/x/net/publicsuffix"
)

func IsCrossOrigin(urlA string, urlB string) bool {
	a, _ := url.Parse(urlA)
	b, _ := url.Parse(urlB)

	if a.Host != b.Host {
		return false
	}

	if a.Scheme != b.Scheme {
		return false
	}

	return true
}

func IsCrossSite(urlA string, urlB string) bool {
	a, _ := url.Parse(urlA)
	b, _ := url.Parse(urlB)

	eTLDPlus1A, _ := publicsuffix.EffectiveTLDPlusOne(a.Hostname())
	eTLDPlus1B, _ := publicsuffix.EffectiveTLDPlusOne(b.Hostname())

	return eTLDPlus1A != eTLDPlus1B
}
