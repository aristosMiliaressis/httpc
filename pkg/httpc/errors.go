package httpc

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
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

func (e TransportError) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.String())
}

// TODO: use replay cache for banned requests
func (c *HttpClient) handleError(evt HttpEvent, err error) HttpEvent {
	var errorCount int

	c.errorMutex.Lock()

	if os.IsTimeout(err) || errors.Is(err, syscall.ETIME) || errors.Is(err, syscall.ETIMEDOUT) {
		evt.TransportError = Timeout
		c.errorLog[Timeout.String()] += 1
		errorCount = c.errorLog[Timeout.String()]
	} else if errors.Is(err, syscall.ECONNRESET) || strings.Contains(err.Error(), "An existing connection was forcibly closed") {
		evt.TransportError = ConnectionReset
		c.errorLog[ConnectionReset.String()] += 1
		errorCount = c.errorLog[ConnectionReset.String()]
	} else if strings.Contains(err.Error(), "invalid header field name") {
		evt.TransportError = UnknownError
	} else {
		gologger.Error().Msg(err.Error())
		evt.TransportError = UnknownError
		c.errorLog[UnknownError.String()] += 1
		errorCount = c.errorLog[UnknownError.String()]
	}

	c.errorMutex.Unlock()

	if errorCount >= c.Options.IpBanDetectionThreshold {
		err = c.verifyIpBan(evt)
	}

	gologger.Debug().Msgf("%s %s\n", evt.Request.URL.String(), evt.TransportError)

	return evt
}

func (c *HttpClient) verifyIpBan(evt HttpEvent) error {

	events := c.EventLog.Search(func(e *HttpEvent) bool {
		if evt.Response == nil {
			return e.TransportError != evt.TransportError
		} else {
			return e.Response != nil && e.Response.StatusCode != evt.Response.StatusCode
		}
	})

	i := 0
	for {
		if i >= len(events) || i > 3 {
			break
		}
		req := events[i].Request.Clone(c.context)

		newEvt := c.Send(req)
		if evt.TransportError != NoError && newEvt.TransportError != evt.TransportError {
			return nil
		} else if evt.TransportError == NoError && newEvt.Response != nil && newEvt.Response.Status != evt.Response.Status {
			return nil
		}

		i += 1
	}

	if c.Options.IpRotateOnIpBan {
		return c.enableIpRotate(evt.Request.URL)
	} else {
		//gologger.Fatal().Msg("IP ban detected, exiting.")
		//os.Exit(1)
	}

	return nil
}
