package httpc

type TransportError int

const (
	NoError TransportError = iota
	DnsError
	TlsNegotiationFailure
	ConnectionReset
	Timeout
)

func (e TransportError) String() string {
	return []string{"NoError", "DnsError", "TlsNegotiationFailure", "ConnectionReset", "Timeout"}[e]
}
