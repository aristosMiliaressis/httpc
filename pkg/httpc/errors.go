package httpc

import (
	"errors"
	"os"
	"syscall"

	"github.com/projectdiscovery/gologger"
)

type TransportError int

const (
	NoError TransportError = iota
	Timeout
	ConnectionReset
	TlsNegotiationFailure
	DnsError
	UnknownError
)

func (e TransportError) String() string {
	return []string{"NoError", "Timeout", "ConnectionReset", "TlsNegotiationFailure", "DnsError", "UnknownError"}[e]
}

// TODO: use replay cache for banned requests
func (c *HttpClient) handleError(evt HttpEvent, err error) HttpEvent {
	var errorCount int

	c.errorMutex.Lock()

	if os.IsTimeout(err) || errors.Is(err, syscall.ETIME) || errors.Is(err, syscall.ETIMEDOUT) {
		evt.TransportError = Timeout
		errorCount = c.errorLog[Timeout.String()] + 1
	} else if errors.Is(err, syscall.ECONNRESET) {
		evt.TransportError = ConnectionReset
		errorCount = c.errorLog[ConnectionReset.String()] + 1
	} else {
		evt.TransportError = UnknownError
		errorCount = c.errorLog[UnknownError.String()] + 1
	}

	c.errorMutex.Unlock()

	if errorCount >= c.Options.IpBanDetectionThreshold {
		err = c.verifyIpBan(evt)
	}

	gologger.Debug().Msgf("%s %s\n", evt.Request.URL.String(), evt.TransportError)

	return evt
}

func (c *HttpClient) verifyIpBan(evt HttpEvent) error {

	// TODO: look in RequestLog for request with different error || status

	if c.Options.IpRotateOnIpBan {
		return c.enableIpRotate(evt.Request.URL)
	}

	return nil
}
