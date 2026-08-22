package platformmcp

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// maxRemoteURLLength bounds user-supplied remote MCP URLs. The URL travels
// inside a signed probe receipt and lands in a registration row, so an
// unbounded value would inflate both surfaces for no legitimate server.
const maxRemoteURLLength = 2048

// ErrRemoteURLInvalid reports a user-supplied remote MCP URL that fails shape
// validation. It is returned before any network I/O and maps to the
// invalid_url tool result code.
var ErrRemoteURLInvalid = errors.New("invalid remote mcp url")

// normalizeRemoteURL validates a user-supplied remote MCP URL and returns its
// normalized form. Validation is purely syntactic and happens before any
// network I/O: https only, no userinfo, no fragment, no query string, and no
// template placeholders (which the registration store also refuses, so
// admitting one here would only defer the refusal to the mutation). Query
// strings are refused outright as a bounded v1 rule: query parameters commonly
// carry credentials, which must never be persisted in a registration row or
// audit event, and the dashboard remains the path for exotic URLs.
// Normalization lowercases the host and strips the default https port so that
// one server has one receipt identity and one registration row identity.
//
// Egress policy (private ranges, denied hosts) is deliberately not consulted
// here; that refusal belongs to the guardian at probe time and maps to
// egress_denied rather than invalid_url.
func normalizeRemoteURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("%w: url is empty", ErrRemoteURLInvalid)
	}
	if len(trimmed) > maxRemoteURLLength {
		return "", fmt.Errorf("%w: url exceeds %d characters", ErrRemoteURLInvalid, maxRemoteURLLength)
	}
	// url.Parse drops an empty fragment ("...#"), so the raw string is the only
	// place a bare fragment delimiter is still visible.
	if strings.Contains(trimmed, "#") {
		return "", fmt.Errorf("%w: fragment is not allowed", ErrRemoteURLInvalid)
	}
	// Checked on the raw string so a bare delimiter ("...?") is refused the
	// same as a populated query.
	if strings.Contains(trimmed, "?") {
		return "", fmt.Errorf("%w: query string is not allowed", ErrRemoteURLInvalid)
	}
	if hasUnresolvedRemoteTemplate(trimmed) {
		return "", fmt.Errorf("%w: template placeholders are not allowed", ErrRemoteURLInvalid)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: not a parseable url", ErrRemoteURLInvalid)
	}
	if parsed.Scheme != "https" {
		return "", fmt.Errorf("%w: scheme must be https", ErrRemoteURLInvalid)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("%w: userinfo is not allowed", ErrRemoteURLInvalid)
	}
	if parsed.Opaque != "" || parsed.Hostname() == "" {
		return "", fmt.Errorf("%w: host is required", ErrRemoteURLInvalid)
	}

	parsed.Host = strings.ToLower(parsed.Host)
	// The default port is compared numerically so zero-padded spellings such
	// as ":0443" collapse to the same receipt and registration identity.
	if port, err := strconv.Atoi(parsed.Port()); err == nil && port == 443 {
		host := parsed.Hostname()
		if strings.Contains(host, ":") {
			// An IPv6 literal loses its brackets through Hostname; restore them.
			host = "[" + host + "]"
		}
		parsed.Host = host
	}
	// A dangling separator with no port ("host:") is the same authority as the
	// bare host.
	parsed.Host = strings.TrimSuffix(parsed.Host, ":")

	return parsed.String(), nil
}
