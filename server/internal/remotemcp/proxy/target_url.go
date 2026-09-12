package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

// ErrInsecureRemoteMCPTransport indicates a hosted Remote MCP URL using cleartext HTTP.
var ErrInsecureRemoteMCPTransport = errors.New("remote MCP URL must use https unless it targets explicit loopback")

// ValidateRemoteMCPURL requires HTTPS for hosted targets while preserving
// explicit loopback HTTP URLs used by local development.
func ValidateRemoteMCPURL(ctx context.Context, policy *guardian.Policy, rawURL string) (*url.URL, error) {
	validated, err := policy.ValidateHTTPURL(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("validate remote MCP URL: %w", err)
	}
	if validated.Scheme == "https" {
		return validated, nil
	}

	switch strings.ToLower(validated.Hostname()) {
	case "127.0.0.1", "::1", "localhost":
		return validated, nil
	default:
		return nil, ErrInsecureRemoteMCPTransport
	}
}
