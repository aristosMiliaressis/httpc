package httpc

import (
	"net/url"

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
