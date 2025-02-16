package httpc

import (
	"encoding/json"
	"errors"
	"fmt"
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
	UnsupportedProtocolScheme
	UnknownError
)

func (e TransportError) String() string {
	return []string{"NoError", "Timeout", "ConnectionReset", "TlsNegotiationFailure", "DnsError", "UnsupportedProtocolScheme", "UnknownError"}[e]
}

func (e TransportError) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.String())
}

func (c *HttpClient) handleTransportError(msg *MessageDuplex, err error) {

	if strings.Contains(err.Error(), "context canceled") {
		return
	}

	c.errorMutex.Lock()
	c.totalErrors += 1
	c.consecutiveErrors += 1

	if os.IsTimeout(err) || errors.Is(err, syscall.ETIME) || errors.Is(err, syscall.ETIMEDOUT) {
		msg.TransportError = Timeout
		c.errorLog[Timeout.String()] += 1
	} else if errors.Is(err, syscall.ECONNRESET) ||
		strings.Contains(err.Error(), "An existing connection was forcibly closed") ||
		strings.Contains(err.Error(), "client connection force closed via ClientConn.Close") ||
		strings.Contains(err.Error(), "server sent GOAWAY and closed the connection") {
		msg.TransportError = ConnectionReset
		c.errorLog[ConnectionReset.String()] += 1
	} else if strings.Contains(err.Error(), "unsupported protocol scheme") {
		msg.TransportError = UnsupportedProtocolScheme
		c.errorLog[UnsupportedProtocolScheme.String()] += 1
	} else {
		gologger.Debug().Msg(err.Error())
		msg.TransportError = UnknownError
		c.errorLog[UnknownError.String()] += 1
	}
	c.errorMutex.Unlock()

	if c.Options.ErrorHandling.ConsecutiveThreshold != 0 &&
		c.consecutiveErrors > c.Options.ErrorHandling.ConsecutiveThreshold {
		exit := true
		if c.Options.ErrorHandling.VerifyIPBanIfExheeded {
			exit = c.verifyIpBan(msg)
		}
		if exit {
			if c.Options.ErrorHandling.IpRotateIfExheeded {
				c.enableIpRotate(msg.Request.URL)
				return
			}
			if c.Options.ErrorHandling.ReportErrorsIfExheeded {
				gologger.Info().Msg(c.GetErrorSummary())
			}
			gologger.Fatal().Msgf("Exceeded %d consecutive errors threshold, exiting.", c.Options.ErrorHandling.ConsecutiveThreshold)
		}
	}

	if c.Options.ErrorHandling.PercentageThreshold != 0 && c.totalSuccessful+c.totalErrors > 40 &&
		(c.totalSuccessful == 0 || int(100.0/(float64((c.totalSuccessful+c.totalErrors))/float64(c.totalErrors))) > c.Options.ErrorHandling.PercentageThreshold) {
		exit := true
		if c.Options.ErrorHandling.VerifyIPBanIfExheeded {
			exit = c.verifyIpBan(msg)
		}
		if exit {
			if c.Options.ErrorHandling.IpRotateIfExheeded {
				c.enableIpRotate(msg.Request.URL)
				return
			}
			if c.Options.ErrorHandling.ReportErrorsIfExheeded {
				gologger.Info().Msg(c.GetErrorSummary())
			}
			gologger.Fatal().Msgf("%d errors out of %d requests exceeded %d%% error threshold, exiting.", c.totalErrors, c.totalSuccessful+c.totalErrors, c.Options.ErrorHandling.PercentageThreshold)
		}
	}

	gologger.Debug().Msgf("%s %s\n", msg.Request.URL.String(), msg.TransportError)
}

func (c *HttpClient) handleHttpError(msg *MessageDuplex) {
	exit := true

	if c.Options.ErrorHandling.ConsecutiveThreshold != 0 &&
		c.consecutiveErrors > c.Options.ErrorHandling.ConsecutiveThreshold {

		if c.Options.ErrorHandling.VerifyIPBanIfExheeded {
			exit = c.verifyIpBan(msg)
		}
		if exit {
			if c.Options.ErrorHandling.IpRotateIfExheeded {
				c.enableIpRotate(msg.Request.URL)
				return
			}
			if c.Options.ErrorHandling.ReportErrorsIfExheeded {
				gologger.Info().Msg(c.GetErrorSummary())
			}
			gologger.Fatal().Msgf("Exceeded %d consecutive errors threshold, exiting.", c.Options.ErrorHandling.ConsecutiveThreshold)
		}
	}

	if c.Options.ErrorHandling.PercentageThreshold != 0 && c.totalSuccessful+c.totalErrors > 40 &&
		(c.totalSuccessful == 0 || int(100.0/(float64((c.totalSuccessful+c.totalErrors))/float64(c.totalErrors))) > c.Options.ErrorHandling.PercentageThreshold) {

		if c.Options.ErrorHandling.VerifyIPBanIfExheeded {
			exit = c.verifyIpBan(msg)
		}
		if exit {
			if c.Options.ErrorHandling.IpRotateIfExheeded {
				c.enableIpRotate(msg.Request.URL)
				return
			}
			if c.Options.ErrorHandling.ReportErrorsIfExheeded {
				gologger.Info().Msg(c.GetErrorSummary())
			}
			gologger.Fatal().Msgf("%d errors out of %d requests exceeded %d%% error threshold, exiting.", c.totalErrors, c.totalSuccessful+c.totalErrors, c.Options.ErrorHandling.PercentageThreshold)
		}
	}
}

func (c *HttpClient) verifyIpBan(msg *MessageDuplex) bool {
	if c.ipBanCheck.Load() {
		return false
	}
	defer c.ipBanCheck.Store(false)
	c.ipBanCheck.Store(true)

	gologger.Warning().Msg("Potential IP ban detected, verifying..")

	messages := c.MessageLog.Search(func(e *MessageDuplex) bool {
		if msg.Response == nil {
			return e.TransportError != msg.TransportError && e.Request != nil
		} else {
			return e.Response != nil && e.Response.StatusCode != msg.Response.StatusCode && e.Request != nil
		}
	})

	if len(messages) == 0 {
		messages = c.MessageLog
	}

	req := messages[0].Request.Clone(c.context)

	opts := c.Options
	opts.RequestPriority = 1000
	opts.Performance.ReplayRateLimitted = false
	newMsg := c.SendWithOptions(req, opts)

	c.ThreadPool.lockedThreads <- true
	<-newMsg.Resolved
	<-c.ThreadPool.lockedThreads

	if msg.TransportError != NoError && newMsg.TransportError != msg.TransportError {
		gologger.Warning().Msg("No IP ban, continuing..")
		return false
	} else if msg.TransportError == NoError && newMsg.Response != nil && newMsg.Response.Status != msg.Response.Status {
		gologger.Warning().Msg("No IP ban, continuing..")
		return false
	}

	gologger.Warning().Msg("IP ban detected, exiting.")

	return true
}

func (c *HttpClient) GetErrorSummary() string {
	timeouts := c.MessageLog.Search(func(e *MessageDuplex) bool {
		return e.TransportError == Timeout
	})

	connectionReset := c.MessageLog.Search(func(e *MessageDuplex) bool {
		return e.TransportError == ConnectionReset
	})

	generalTransportError := c.MessageLog.Search(func(e *MessageDuplex) bool {
		return e.TransportError == UnknownError
	})

	httpErrors := c.MessageLog.Search(func(e *MessageDuplex) bool {
		return e.Response != nil && e.Response.StatusCode >= 400 && c.Options.ErrorHandling.Matches(e.Response.StatusCode)
	})

	groupedHttpErrors := map[int]int{}
	for _, errorResponse := range httpErrors {
		groupedHttpErrors[errorResponse.Response.StatusCode] += 1
	}

	errorPercentage := int(100.0 / (float64((c.totalSuccessful + c.totalErrors)) / float64(c.totalErrors)))

	errorTypes := []string{}
	for status, count := range groupedHttpErrors {
		errorTypes = append(errorTypes, fmt.Sprintf("%d: %d", status, count))
	}
	if len(timeouts) != 0 {
		errorTypes = append(errorTypes, fmt.Sprintf("Timeouts: %d", len(timeouts)))
	}
	if len(connectionReset) != 0 {
		errorTypes = append(errorTypes, fmt.Sprintf("ConnectionReset: %d", len(connectionReset)))
	}
	if len(generalTransportError) != 0 {
		errorTypes = append(errorTypes, fmt.Sprintf("GenericTransportError: %d", len(generalTransportError)))
	}
	return fmt.Sprintf("successful: %d, failed: %d, consecutive errors: %d, percentage: %d%% (%s)\n",
		c.totalSuccessful, c.totalErrors, c.consecutiveErrors, errorPercentage, strings.Join(errorTypes[:], ","))
}

// func isRSTError(err error) bool {
// 	// case *http2.RSTStreamFrame:
// 	// 	err = ConnDropError{Wrapped: fmt.Errorf("error code %v", f.ErrCode)}

// 	// if ga, ok := f.(*http2.GoAwayFrame); ok {
// 	// 	err = ConnDropError{
// 	// 		Wrapped: fmt.Errorf("received GOAWAY: error code %v", ga.ErrCode),
// 	// 	}
// 	// 	return
// 	// }

// 	_, ok := err.(ConnDropError)
// 	return ok
// }

// type ConnDropError struct {
// 	Wrapped error
// }

// func (r ConnDropError) Error() string {
// 	return fmt.Sprintf("server dropped connection, error=%v", r.Wrapped)
// }

//	func isTimeoutError(err error) bool {
//		n, ok := err.(net.Error)
//		if !ok {
//			return false
//		}
//		return n.Timeout()
//	}
// type TransportErrorPseudoCode int

// const (
// 	NoError TransportErrorPseudoCode = iota + 600
// 	Timeout
// 	ConnectionReset
// 	TlsNegotiationFailure
// 	DnsError
// 	UnknownError
// )
