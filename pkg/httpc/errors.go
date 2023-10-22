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
var possiblyProblematicErrorCodes = []int{400, 403, 409, 402, 418, 420, 450, 451, 503, 504, 525, 530}

func (c *HttpClient) handleTransportError(msg *MessageDuplex, err error) *MessageDuplex {

	c.errorMutex.Lock()
	c.totalErrors += 1
	c.consecutiveErrors += 1

	if os.IsTimeout(err) || errors.Is(err, syscall.ETIME) || errors.Is(err, syscall.ETIMEDOUT) {
		msg.TransportError = Timeout
		c.errorLog[Timeout.String()] += 1
	} else if errors.Is(err, syscall.ECONNRESET) || strings.Contains(err.Error(), "An existing connection was forcibly closed") {
		msg.TransportError = ConnectionReset
		c.errorLog[ConnectionReset.String()] += 1
		// } else if strings.Contains(err.Error(), "invalid header field name") {
		// 	msg.TransportError = UnknownError
	} else {
		gologger.Error().Msg(err.Error())
		msg.TransportError = UnknownError
		c.errorLog[UnknownError.String()] += 1
	}

	if c.Options.ErrorHandling.ConsecutiveErrorThreshold != 0 &&
		c.consecutiveErrors > c.Options.ErrorHandling.ConsecutiveErrorThreshold {
		gologger.Fatal().Msgf("Exceeded %d consecutive errors threshold, exiting.", c.Options.ErrorHandling.ConsecutiveErrorThreshold)
		os.Exit(1)
	}
	c.errorMutex.Unlock()

	if c.Options.ErrorHandling.ErrorPercentageThreshold != 0 &&
		c.totalSuccessful+c.totalErrors > 40 &&
		(c.totalSuccessful == 0 || int(100.0/(float64((c.totalSuccessful+c.totalErrors))/float64(c.totalErrors))) > c.Options.ErrorHandling.ErrorPercentageThreshold) {
		gologger.Fatal().Msgf("%d errors out of %d requests exceeded %d%% error threshold, exiting.", c.totalErrors, c.totalSuccessful+c.totalErrors, c.Options.ErrorHandling.ErrorPercentageThreshold)
		os.Exit(1)
	}

	gologger.Debug().Msgf("%s %s\n", msg.Request.URL.String(), msg.TransportError)

	return msg
}

func (c *HttpClient) handleHttpError(msg *MessageDuplex) {
	if c.Options.ErrorHandling.ConsecutiveErrorThreshold != 0 &&
		c.consecutiveErrors > c.Options.ErrorHandling.ConsecutiveErrorThreshold {
		gologger.Fatal().Msgf("Exceeded %d consecutive errors threshold, exiting.", c.Options.ErrorHandling.ConsecutiveErrorThreshold)
		os.Exit(1)
	}

	if c.Options.ErrorHandling.ErrorPercentageThreshold != 0 &&
		c.totalSuccessful+c.totalErrors > 40 &&
		(c.totalSuccessful == 0 || int(100.0/(float64((c.totalSuccessful+c.totalErrors))/float64(c.totalErrors))) > c.Options.ErrorHandling.ErrorPercentageThreshold) {
		gologger.Fatal().Msgf("%d errors out of %d requests exceeded %d%% error threshold, exiting.", c.totalErrors, c.totalSuccessful+c.totalErrors, c.Options.ErrorHandling.ErrorPercentageThreshold)
		os.Exit(1)
	}
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

	if c.Options.ErrorHandling.IpRotateIfThresholdExheeded {
		return c.enableIpRotate(msg.Request.URL)
	} else {
		//gologger.Fatal().Msg("IP ban detected, exiting.")
	}

	return nil
}
