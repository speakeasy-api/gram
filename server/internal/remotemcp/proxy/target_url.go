package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

var (
	// ErrInsecureRemoteMCPTransport indicates a hosted Remote MCP URL using cleartext HTTP.
	ErrInsecureRemoteMCPTransport = errors.New("remote MCP URL must use https unless it targets explicit loopback")
	// ErrRemoteMCPURLUserinfo indicates a Remote MCP URL containing embedded credentials.
	ErrRemoteMCPURLUserinfo = errors.New("remote MCP URL must not contain userinfo")
)

// ValidateRemoteMCPURL applies transport requirements and the Guardian policy.
// It is intended for management-time validation and explicit probes; ordinary
// runtime requests use validateRemoteMCPTransportURL and rely on Guardian's
// dialer for DNS and IP enforcement.
func ValidateRemoteMCPURL(ctx context.Context, policy *guardian.Policy, rawURL string) (*url.URL, error) {
	if _, err := validateRemoteMCPTransportURL(rawURL); err != nil {
		return nil, err
	}

	validated, err := policy.ValidateHTTPURL(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("validate remote MCP URL: %w", err)
	}
	return validated, nil
}

// validateRemoteMCPTransportURL performs no DNS lookup. Runtime SSRF
// enforcement remains in Guardian's dialer, including redirected requests.
func validateRemoteMCPTransportURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse remote MCP URL: %w", err)
	}
	if parsed.User != nil {
		return nil, ErrRemoteMCPURLUserinfo
	}
	if parsed.Host == "" {
		return nil, errors.New("remote MCP URL must include a host")
	}

	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return parsed, nil
	case "http":
		host := parsed.Hostname()
		if strings.EqualFold(host, "localhost") {
			return parsed, nil
		}
		if zoneStart := strings.LastIndexByte(host, '%'); zoneStart >= 0 {
			host = host[:zoneStart]
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return parsed, nil
		}
		return nil, ErrInsecureRemoteMCPTransport
	default:
		return nil, errors.New("remote MCP URL scheme must be http or https")
	}
}
