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

var safeErrorsList = []int{401, 402, 404, 405, 406, 407, 410, 411, 412, 413, 414, 415, 416, 417, 426, 431, 429, 500, 501}

// TODO: use replay cache for banned requests
func (c *HttpClient) handleError(msg *MessageDuplex, err error) *MessageDuplex {
	//var errorCount int

	c.errorMutex.Lock()

	if os.IsTimeout(err) || errors.Is(err, syscall.ETIME) || errors.Is(err, syscall.ETIMEDOUT) {
		msg.TransportError = Timeout
		c.errorLog[Timeout.String()] += 1
		//errorCount = c.errorLog[Timeout.String()]
	} else if errors.Is(err, syscall.ECONNRESET) || strings.Contains(err.Error(), "An existing connection was forcibly closed") {
		msg.TransportError = ConnectionReset
		c.errorLog[ConnectionReset.String()] += 1
		//errorCount = c.errorLog[ConnectionReset.String()]
	} else if strings.Contains(err.Error(), "invalid header field name") {
		msg.TransportError = UnknownError
	} else {
		gologger.Error().Msg(err.Error())
		msg.TransportError = UnknownError
		c.errorLog[UnknownError.String()] += 1
		//errorCount = c.errorLog[UnknownError.String()]
	}

	c.errorMutex.Unlock()

	// if errorCount >= c.Options.ErrorHandling.IpBanDetectionThreshold {
	// 	err = c.verifyIpBan(msg)
	// }

	gologger.Debug().Msgf("%s %s\n", msg.Request.URL.String(), msg.TransportError)

	return msg
}

func (c *HttpClient) verifyIpBan(msg *MessageDuplex) error {

	messages := c.MessageLog.Search(func(e *MessageDuplex) bool {
		if msg.Response == nil {
			return e.TransportError != msg.TransportError && e.Request != nil
		} else {
			return e.Response != nil && e.Response.StatusCode != msg.Response.StatusCode && e.Request != nil
		}
	})

	i := 0
	for {
		if i >= len(messages) || i > 3 {
			break
		}
		req := messages[i].Request.Clone(c.context)

		newMsg := c.Send(req)
		if msg.TransportError != NoError && newMsg.TransportError != msg.TransportError {
			return nil
		} else if msg.TransportError == NoError && newMsg.Response != nil && newMsg.Response.Status != msg.Response.Status {
			return nil
		}

		i += 1
	}

	if c.Options.ErrorHandling.IpRotateOnIpBan {
		return c.enableIpRotate(msg.Request.URL)
	} else {
		//gologger.Fatal().Msg("IP ban detected, exiting.")
	}

	return nil
}
