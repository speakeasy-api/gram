package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/net/http/httpguts"

	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz"
)

// HeaderPolicy selects how strictly a proxy treats operator-configured headers
// and the client headers it copies upstream.
type HeaderPolicy string

const (
	// HeaderPolicyRemote is the policy for remote MCP servers. It keeps the
	// long-standing remote forwarding rules: assertion and standard MCP
	// headers are protected, Cookie/Set-Cookie/Proxy-Authorization cannot be
	// pass-through sources, and nothing else is filtered.
	HeaderPolicyRemote HeaderPolicy = ""

	// HeaderPolicyTunneled is the strict policy for tunneled MCP servers.
	// Speakeasy credentials, tunnel transport fields and caller assertions
	// never reach the upstream, either copied from the client or read into a
	// configured header, and configured headers may not claim transport,
	// framing or MCP protocol names.
	HeaderPolicyTunneled HeaderPolicy = "tunneled"
)

// ErrInvalidHeaderName reports a configured header name that is not a valid
// HTTP field name.
var ErrInvalidHeaderName = errors.New("invalid header name")

// ErrInvalidHeaderValue reports a configured header value containing bytes
// that cannot appear in an HTTP field value.
var ErrInvalidHeaderValue = errors.New("invalid header value")

// ErrReservedHeader reports a configured header whose name or source is
// reserved by the policy.
var ErrReservedHeader = errors.New("reserved header")

// NormalizeHeaderName validates raw as an HTTP field name and returns its
// canonical form. Control bytes anywhere in raw are rejected before any
// trimming, so a name carrying CR, LF or a tab is never quietly repaired into
// a valid one. Leading and trailing ASCII spaces are trimmed.
func NormalizeHeaderName(raw string) (string, error) {
	for i := range len(raw) {
		if c := raw[i]; c < 0x20 || c == 0x7f {
			return "", fmt.Errorf("%w: contains a control character", ErrInvalidHeaderName)
		}
	}
	name := strings.Trim(raw, " ")
	if !httpguts.ValidHeaderFieldName(name) {
		return "", fmt.Errorf("%w: %q is not an HTTP field name", ErrInvalidHeaderName, name)
	}
	return http.CanonicalHeaderKey(name), nil
}

// ValidateHeaderValue reports whether v may be sent as an HTTP field value.
// Horizontal tab is the only control character allowed.
func ValidateHeaderValue(v string) error {
	for i := range len(v) {
		if c := v[i]; (c < 0x20 && c != '\t') || c == 0x7f {
			return fmt.Errorf("%w: contains a control character", ErrInvalidHeaderValue)
		}
	}
	if !httpguts.ValidHeaderFieldValue(v) {
		return ErrInvalidHeaderValue
	}
	return nil
}

// headerKey folds a header name for policy matching. Underscores are treated
// as dashes because some upstream servers read X_Foo as X-Foo.
func headerKey(name string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), "_", "-"))
}

// speakeasyHeaderPrefix covers Speakeasy's own request headers: API keys,
// dashboard and chat sessions, project selection and consent transport state.
const speakeasyHeaderPrefix = "gram-"

// speakeasyAIHeaderPrefix covers the documented Speakeasy-AI-* names for the
// same headers. Middleware rewrites them to their older aliases before the
// proxy runs, but the policy must not depend on that ordering.
const speakeasyAIHeaderPrefix = "speakeasy-ai-"

// tunnelHeaderPrefix covers the tunnel transport family exchanged between
// Speakeasy, the tunnel gateway and the tunnel agent.
const tunnelHeaderPrefix = "x-gram-tunnel-"

// IsProtectedInboundHeader reports whether a header on the inbound request
// carries a Speakeasy credential, session, caller assertion or tunnel
// transport field. Under [HeaderPolicyTunneled] such a header is never copied
// upstream and never used as the source of a configured header.
func IsProtectedInboundHeader(name string) bool {
	if mcpauthz.ReservedHeader(name) {
		return true
	}
	key := headerKey(name)
	if strings.HasPrefix(key, speakeasyHeaderPrefix) || strings.HasPrefix(key, speakeasyAIHeaderPrefix) || strings.HasPrefix(key, tunnelHeaderPrefix) {
		return true
	}
	switch key {
	case
		"authorization",
		"proxy-authorization",
		"cookie",
		"set-cookie",
		"x-gram-agent-version":
		return true
	}
	return false
}

// isReservedTunneledDestination reports whether a configured header may not
// be sent under name to a tunneled upstream. Authorization is allowed: a
// static service credential is a legitimate configuration, and a resolved
// upstream token still takes precedence over it.
func isReservedTunneledDestination(name string) bool {
	if httpheaders.IsStandardMCPRequestHeader(strings.ReplaceAll(name, "_", "-")) {
		return true
	}
	key := headerKey(name)
	if key == "authorization" {
		return false
	}
	if IsProtectedInboundHeader(name) {
		return true
	}
	switch key {
	case
		// Hop-by-hop and framing fields belong to the HTTP transport.
		"connection",
		"keep-alive",
		"proxy-authenticate",
		"te",
		"trailer",
		"transfer-encoding",
		"upgrade",
		"content-length",
		"host",
		"accept-encoding",
		// MCP Streamable HTTP session and resumption fields.
		"mcp-session-id",
		"last-event-id":
		return true
	}
	return false
}

// CheckTunneledHeader validates a configured header against
// [HeaderPolicyTunneled]. h.Name and h.ValueFromRequestHeader must already be
// normalized with [NormalizeHeaderName]; a static value is checked with
// [ValidateHeaderValue].
func CheckTunneledHeader(h ConfiguredHeader) error {
	if isReservedTunneledDestination(h.Name) {
		return fmt.Errorf("%w: %q cannot be configured on a tunneled MCP server", ErrReservedHeader, h.Name)
	}
	if h.ValueFromRequestHeader != "" && IsProtectedInboundHeader(h.ValueFromRequestHeader) {
		return fmt.Errorf("%w: %q cannot be forwarded to a tunneled MCP server", ErrReservedHeader, h.ValueFromRequestHeader)
	}
	if h.StaticValue != "" {
		if err := ValidateHeaderValue(h.StaticValue); err != nil {
			return err
		}
	}
	return nil
}

// checkStoredTunneledHeader re-validates a stored configured header at request
// time, including its name and source syntax, so a row written before the
// policy existed, or by another writer, cannot bypass it.
func checkStoredTunneledHeader(h ConfiguredHeader) error {
	name, err := NormalizeHeaderName(h.Name)
	if err != nil {
		return err
	}
	if name != h.Name {
		return fmt.Errorf("%w: %q is not in canonical form", ErrInvalidHeaderName, h.Name)
	}
	if h.ValueFromRequestHeader != "" {
		if _, err := NormalizeHeaderName(h.ValueFromRequestHeader); err != nil {
			return err
		}
	}
	return CheckTunneledHeader(h)
}
