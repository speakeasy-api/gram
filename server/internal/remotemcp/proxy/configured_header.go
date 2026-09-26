package proxy

import (
	"fmt"
	"net/http"
	"strings"
)

// isDeniedPassThroughSource reports whether an inbound header may not be used
// as a pass-through source.
//
// isSkippedRequestHeader already refuses to copy Cookie upstream because it
// carries the dashboard's gram_session rather than any intent toward the
// remote server. A configured header reads the inbound request directly, so
// without this it could carry that same cookie upstream under any name it
// likes and walk straight around that decision.
//
// Authorization is deliberately absent: forwarding the caller's own upstream
// credential is what pass-through identity is for.
func isDeniedPassThroughSource(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "cookie", "set-cookie", "proxy-authorization":
		return true
	default:
		return false
	}
}

// ConfiguredHeader describes how a single outgoing header sent to the remote
// MCP server is populated. Exactly one of StaticValue or ValueFromRequestHeader
// is set.
type ConfiguredHeader struct {
	// IsRequired, when true, causes the proxy to reject the request with a
	// bad-request error if the header cannot be resolved to a non-empty value.
	IsRequired bool

	// Name is the HTTP header name to send to the remote MCP server.
	Name string

	// StaticValue holds a fixed value (already decrypted if originally a
	// secret header). Leave empty when ValueFromRequestHeader is set.
	StaticValue string

	// ValueFromRequestHeader names a header on the inbound user request whose
	// value should be forwarded. Leave empty when StaticValue is set.
	ValueFromRequestHeader string
}

// Resolve returns the value to send upstream for this header, either pulled
// from the user request when ValueFromRequestHeader is set or taken from
// StaticValue. Returns an error when IsRequired is true but no non-empty
// value can be produced.
func (h ConfiguredHeader) Resolve(userReq *http.Request) (string, error) {
	switch {
	case h.ValueFromRequestHeader != "":
		if isDeniedPassThroughSource(h.ValueFromRequestHeader) {
			return "", fmt.Errorf("header %q cannot be populated from request header %q", h.Name, h.ValueFromRequestHeader)
		}
		value := userReq.Header.Get(h.ValueFromRequestHeader)
		if value == "" && h.IsRequired {
			return "", fmt.Errorf("required header %q missing from request (pass-through from %q)", h.Name, h.ValueFromRequestHeader)
		}
		return value, nil
	case h.StaticValue != "":
		return h.StaticValue, nil
	default:
		if h.IsRequired {
			return "", fmt.Errorf("required header %q has no configured value", h.Name)
		}
		return "", nil
	}
}
