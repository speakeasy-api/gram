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
	// HeaderPolicyRemote is the policy for remote MCP servers and the zero
	// value, so a proxy built without an explicit policy gets it. Speakeasy
	// credentials, sessions, caller assertions and tunnel transport fields
	// are never copied from the client upstream nor read into a configured
	// header, and configured headers may not set Set-Cookie or populate
	// Cookie from the inbound request. A static Cookie remains allowed as an
	// operator-supplied upstream credential.
	HeaderPolicyRemote HeaderPolicy = ""

	// HeaderPolicyTunneled is the policy for tunneled MCP servers. Their
	// configured headers carry Speakeasy's own tunnel routing fields, so the
	// remote filtering does not apply to them.
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
	if strings.HasPrefix(key, speakeasyHeaderPrefix) || strings.HasPrefix(key, tunnelHeaderPrefix) {
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

// ErrProtectedSource reports a configured header that reads a protected inbound
// header. It always accompanies [ErrReservedHeader].
var ErrProtectedSource = errors.New("protected source")

// CheckRemoteHeader validates a configured header against
// [HeaderPolicyRemote]. Names are matched case-insensitively and need not be
// in canonical form, so a header stored before names were normalized keeps
// working.
func CheckRemoteHeader(h ConfiguredHeader) error {
	if h.ValueFromRequestHeader != "" && IsProtectedInboundHeader(h.ValueFromRequestHeader) {
		return fmt.Errorf("%w: %w: %q cannot be forwarded to a remote MCP server", ErrReservedHeader, ErrProtectedSource, h.ValueFromRequestHeader)
	}
	if isReservedRemoteDestination(h) {
		return fmt.Errorf("%w: %q cannot be configured on a remote MCP server", ErrReservedHeader, h.Name)
	}
	if h.StaticValue != "" {
		if err := ValidateHeaderValue(h.StaticValue); err != nil {
			return err
		}
	}
	return nil
}

// isReservedRemoteDestination reports whether a configured header may not be
// sent under its name to a remote upstream. Cookie is allowed only as a static
// value: an operator credential is legitimate, but populating it from the
// inbound request would hand the upstream whatever cookie the caller sent.
func isReservedRemoteDestination(h ConfiguredHeader) bool {
	if mcpauthz.ReservedHeader(h.Name) || httpheaders.IsStandardMCPRequestHeader(strings.ReplaceAll(h.Name, "_", "-")) {
		return true
	}
	switch headerKey(h.Name) {
	case "set-cookie", "proxy-authorization":
		return true
	case "cookie":
		return h.ValueFromRequestHeader != ""
	}
	return false
}

// checkStoredRemoteName validates a stored header name or source at request
// time. Any casing is accepted, so a name stored before writes were
// canonicalized keeps working, but surrounding whitespace is not: the name is
// sent and later removed exactly as stored.
func checkStoredRemoteName(raw string) error {
	name, err := NormalizeHeaderName(raw)
	if err != nil {
		return err
	}
	if !strings.EqualFold(name, raw) {
		return fmt.Errorf("%w: %q has surrounding whitespace", ErrInvalidHeaderName, raw)
	}
	return nil
}

// checkStoredRemoteHeader re-validates a stored configured header against
// [HeaderPolicyRemote] at request time.
func checkStoredRemoteHeader(h ConfiguredHeader) error {
	if err := checkStoredRemoteName(h.Name); err != nil {
		return err
	}
	if h.ValueFromRequestHeader != "" {
		if err := checkStoredRemoteName(h.ValueFromRequestHeader); err != nil {
			return err
		}
	}
	return CheckRemoteHeader(h)
}
