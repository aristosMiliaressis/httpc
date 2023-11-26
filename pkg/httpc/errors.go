package httpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/aristosMiliaressis/httpc/internal/util"
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

var safeErrorsList = []int{401, 402, 404, 405, 406, 407, 410, 411, 412, 413, 414, 415, 416, 417, 426, 431, 500, 501}

//var possiblyProblematicErrorCodes = []int{400, 403, 409, 402, 418, 420, 450, 451, 503, 504, 525, 530}

func (c *HttpClient) handleTransportError(msg *MessageDuplex, err error) {

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
	} else {
		gologger.Error().Msg(err.Error())
		msg.TransportError = UnknownError
		c.errorLog[UnknownError.String()] += 1
	}

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
				c.printErrorReport()
			}
			gologger.Fatal().Msgf("Exceeded %d consecutive errors threshold, exiting.", c.Options.ErrorHandling.ConsecutiveThreshold)
		}
	}
	c.errorMutex.Unlock()

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
				c.printErrorReport()
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
				c.printErrorReport()
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
				c.printErrorReport()
			}
			gologger.Fatal().Msgf("%d errors out of %d requests exceeded %d%% error threshold, exiting.", c.totalErrors, c.totalSuccessful+c.totalErrors, c.Options.ErrorHandling.PercentageThreshold)
		}
	}
}

func (c *HttpClient) verifyIpBan(msg *MessageDuplex) bool {

	gologger.Warning().Msg("Potential IP ban detected, verifying..")

	messages := c.MessageLog.Search(func(e *MessageDuplex) bool {
		if msg.Response == nil {
			return e.TransportError != msg.TransportError && e.Request != nil
		} else {
			return e.Response != nil && e.Response.StatusCode != msg.Response.StatusCode && e.Request != nil
		}
	})

	if len(messages) == 0 {
		gologger.Warning().Msg("IP ban detected, exiting.")

		return true
	}

	fmt.Println("BEFORE")
	c.ThreadPool.Rate.Stop()
	fmt.Println("After")

	<-time.After(time.Second * 5)
	fmt.Printf("After2 %d\n", len(messages))

	req := messages[0].Request.Clone(c.context)

	opts := c.Options
	opts.RequestPriority = 1000
	fmt.Println("After3")

	newMsg := c.Send(req)
	fmt.Println("After4")

	c.ThreadPool.Rate.ChangeRate(1)
	fmt.Println("After5")

	<-newMsg.Resolved
	fmt.Println("After6")

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

func (c *HttpClient) printErrorReport() {
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
		return e.Response != nil && e.Response.StatusCode >= 400 && !util.Contains(safeErrorsList, e.Response.StatusCode)
	})

	groupedHttpErrors := map[int]int{}
	for _, errorResponse := range httpErrors {
		groupedHttpErrors[errorResponse.Response.StatusCode] += 1
	}

	fmt.Printf("Timeouts: %d, ConnectionReset: %d, GenericTransportError: %d\n", len(timeouts), len(connectionReset), len(generalTransportError))
	for status, count := range groupedHttpErrors {
		fmt.Printf("%d: %d, ", status, count)
	}
	fmt.Printf("failed: %d, successful: %d\n", c.totalErrors, c.totalSuccessful)
}
