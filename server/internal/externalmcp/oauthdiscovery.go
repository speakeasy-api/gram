package externalmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// OAuthVersion represents the detected OAuth version/capability level.
const (
	OAuthVersionNone = "none" // No OAuth required
	OAuthVersion21   = "2.1"  // MCP OAuth with RFC 8414 discovery + dynamic registration
	OAuthVersion20   = "2.0"  // Legacy OAuth 2.0 (no AS discovery, requires static client config)
)

// OAuthDiscoveryResult contains the OAuth metadata discovered for an external MCP server.
type OAuthDiscoveryResult struct {
	Version               string // "2.1", "2.0", or "none"
	AuthorizationEndpoint string
	TokenEndpoint         string
	RegistrationEndpoint  string
	ScopesSupported       []string

	// ProbeIncomplete reports that at least one discovery request failed at
	// the transport level (unreachable host, TLS failure, 5xx) rather than
	// answering 404/410. A Version of "none" with ProbeIncomplete set means
	// discovery could not run to completion — callers that treat "none" as
	// "publishes no OAuth metadata" must keep that case distinct.
	ProbeIncomplete bool
}

// ExternalMCPOAuthConfig contains OAuth configuration extracted from an external MCP tool
// that requires OAuth authentication.
type ExternalMCPOAuthConfig struct {
	// RemoteURL is the parsed URL of the external MCP server
	RemoteURL *url.URL
	// RegistryID is the ID of the MCP registry the server belongs to
	RegistryID string
	// Slug is the tool prefix slug (e.g., "github")
	Slug string
	// Name is the reverse-DNS server name (e.g., "ai.exa/exa")
	Name string

	// OAuth metadata from the external server
	OAuthVersion          string   // "2.1", "2.0", or "none"
	AuthorizationEndpoint string   // OAuth authorization endpoint URL
	TokenEndpoint         string   // OAuth token endpoint URL
	RegistrationEndpoint  string   // OAuth dynamic client registration endpoint URL
	ScopesSupported       []string // OAuth scopes supported by the server
}

// ResolveOAuthConfig returns the OAuth configuration from the first external MCP tool
// in the toolset that requires OAuth, or nil if none found.
func ResolveOAuthConfig(toolset *types.Toolset) *ExternalMCPOAuthConfig {
	for _, tool := range toolset.Tools {
		if tool.ExternalMcpToolDefinition == nil || !tool.ExternalMcpToolDefinition.RequiresOauth {
			continue
		}

		remoteURL, err := url.Parse(tool.ExternalMcpToolDefinition.RemoteURL)
		if err != nil {
			continue
		}

		def := tool.ExternalMcpToolDefinition
		return &ExternalMCPOAuthConfig{
			RemoteURL:             remoteURL,
			RegistryID:            def.RegistryID,
			Slug:                  def.Slug,
			Name:                  def.Name,
			OAuthVersion:          def.OauthVersion,
			AuthorizationEndpoint: conv.PtrValOr(def.OauthAuthorizationEndpoint, ""),
			TokenEndpoint:         conv.PtrValOr(def.OauthTokenEndpoint, ""),
			RegistrationEndpoint:  conv.PtrValOr(def.OauthRegistrationEndpoint, ""),
			ScopesSupported:       def.OauthScopesSupported,
		}
	}

	return nil
}

// errWellKnownAbsent marks a well-known fetch the server answered with a 4xx:
// the metadata is deliberately not served there, as opposed to a transport
// failure that leaves publication unknown.
var errWellKnownAbsent = errors.New("well-known metadata not published")

// authServerMetadata represents the OAuth 2.0 Authorization Server Metadata (RFC 8414).
type authServerMetadata struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	RegistrationEndpoint  string   `json:"registration_endpoint,omitempty"`
	ScopesSupported       []string `json:"scopes_supported,omitempty"`
}

// protectedResourceMetadata represents OAuth 2.0 Protected Resource Metadata (RFC 9728).
type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported,omitempty"`
}

// DiscoverOAuthMetadata discovers OAuth configuration for an external MCP server.
// It parses the WWW-Authenticate header and fetches metadata from discovered URLs.
// If no metadata URLs are in the header, it probes standard well-known locations.
func DiscoverOAuthMetadata(ctx context.Context, logger *slog.Logger, guardianPolicy *guardian.Policy, wwwAuthenticate string, remoteURL string) (*OAuthDiscoveryResult, error) {
	// Parse the WWW-Authenticate header
	params := parseWWWAuthenticate(wwwAuthenticate)

	var resourceMeta *protectedResourceMetadata
	var authServerMeta *authServerMetadata

	// A 404/410 on a well-known URL is the server cleanly saying the metadata
	// is not published; anything else — unreachable host, TLS failure, 5xx —
	// means discovery did not run to completion. The distinction is carried
	// out on ProbeIncomplete so a dead host never reads as "publishes no
	// OAuth metadata".
	probeFailed := false
	fetchPR := func(u string) *protectedResourceMetadata {
		meta, err := fetchJSON[protectedResourceMetadata](ctx, logger, guardianPolicy, u)
		if err != nil && !errors.Is(err, errWellKnownAbsent) {
			probeFailed = true
		}
		return meta
	}
	fetchAS := func(u string) *authServerMetadata {
		meta, err := fetchJSON[authServerMetadata](ctx, logger, guardianPolicy, u)
		if err != nil && !errors.Is(err, errWellKnownAbsent) {
			probeFailed = true
		}
		return meta
	}

	// Strategy 1: Check for auth_server_metadata in header (direct AS metadata URL)
	if asURL, ok := params["auth_server_metadata"]; ok && asURL != "" {
		if meta := fetchAS(asURL); meta != nil {
			authServerMeta = meta
		}
	}

	// Strategy 2: Check for resource_metadata in header (Protected Resource metadata)
	if rmURL, ok := params["resource_metadata"]; ok && rmURL != "" {
		if meta := fetchPR(rmURL); meta != nil {
			resourceMeta = meta
			// Follow the chain to get AS metadata
			if len(meta.AuthorizationServers) > 0 {
				if asMeta := fetchAS(buildWellKnownURL(meta.AuthorizationServers[0])); asMeta != nil {
					authServerMeta = asMeta
				}
			}
		}
	}

	// Strategy 3: Probe well-known locations derived from the remote URL
	if authServerMeta == nil {
		// Try OAuth Protected Resource metadata first
		if meta := fetchPR(buildWellKnownResourceURL(remoteURL)); meta != nil {
			resourceMeta = meta
			// Follow the chain
			if len(meta.AuthorizationServers) > 0 {
				if asMeta := fetchAS(buildWellKnownURL(meta.AuthorizationServers[0])); asMeta != nil {
					authServerMeta = asMeta
				}
			}
		}

		// Try OAuth Authorization Server metadata directly
		if authServerMeta == nil {
			if asMeta := fetchAS(buildWellKnownURL(remoteURL)); asMeta != nil {
				authServerMeta = asMeta
			}
		}
	}

	// Strategy 4: Some AS servers (e.g. Atlassian) host metadata at the origin
	// root regardless of the MCP endpoint path. When the remote URL has a path
	// and prior strategies found nothing, retry both probes with path stripped.
	if authServerMeta == nil {
		if u, err := url.Parse(remoteURL); err == nil && u.Path != "" && u.Path != "/" {
			rootURL := u.Scheme + "://" + u.Host

			if meta := fetchPR(buildWellKnownResourceURL(rootURL)); meta != nil {
				resourceMeta = meta
				if len(meta.AuthorizationServers) > 0 {
					if asMeta := fetchAS(buildWellKnownURL(meta.AuthorizationServers[0])); asMeta != nil {
						authServerMeta = asMeta
					}
				}
			}

			if authServerMeta == nil {
				if asMeta := fetchAS(buildWellKnownURL(rootURL)); asMeta != nil {
					authServerMeta = asMeta
				}
			}
		}
	}

	// Determine the OAuth version based on what we found
	result := &OAuthDiscoveryResult{
		Version:               OAuthVersionNone,
		AuthorizationEndpoint: "",
		TokenEndpoint:         "",
		RegistrationEndpoint:  "",
		ScopesSupported:       nil,
		ProbeIncomplete:       probeFailed,
	}

	if authServerMeta != nil {
		result.AuthorizationEndpoint = authServerMeta.AuthorizationEndpoint
		result.TokenEndpoint = authServerMeta.TokenEndpoint
		result.RegistrationEndpoint = authServerMeta.RegistrationEndpoint
		result.ScopesSupported = authServerMeta.ScopesSupported
		if resourceMeta != nil && len(resourceMeta.ScopesSupported) > 0 {
			// Protected-resource scopes describe the permissions accepted by
			// this MCP server. Prefer them over the authorization server's
			// potentially broader list, and retain them when the AS omits
			// scopes_supported entirely.
			result.ScopesSupported = resourceMeta.ScopesSupported
		}

		// If we have a registration endpoint, it's full MCP OAuth (2.1)
		// Otherwise it's legacy OAuth 2.0
		if authServerMeta.RegistrationEndpoint != "" {
			result.Version = OAuthVersion21
		} else {
			result.Version = OAuthVersion20
		}
	} else if resourceMeta != nil {
		// We have protected resource metadata but couldn't get AS metadata
		// This means the AS doesn't support RFC 8414 discovery (like GitHub)
		result.Version = OAuthVersion20
		result.ScopesSupported = resourceMeta.ScopesSupported
	}

	return result, nil
}

// parseWWWAuthenticate parses a WWW-Authenticate header and extracts key-value pairs.
// Example: Bearer realm="OAuth", error="invalid_token", resource_metadata="https://..."
func parseWWWAuthenticate(header string) map[string]string {
	params := make(map[string]string)
	if header == "" {
		return params
	}

	// Skip the scheme (e.g., "Bearer ")
	if idx := strings.Index(header, " "); idx != -1 {
		header = header[idx+1:]
	}

	// Parse key="value" pairs
	re := regexp.MustCompile(`(\w+)="([^"]*)"`)
	matches := re.FindAllStringSubmatch(header, -1)
	for _, match := range matches {
		if len(match) == 3 {
			params[match[1]] = match[2]
		}
	}

	return params
}

// buildWellKnownURL constructs the well-known OAuth Authorization Server metadata URL.
// Per RFC 8414 Section 3, the well-known suffix is inserted between the host and the path.
// e.g. https://example.com/path → https://example.com/.well-known/oauth-authorization-server/path
func buildWellKnownURL(baseURL string) string {
	return buildWellKnownSuffixURL(baseURL, "oauth-authorization-server")
}

// buildWellKnownResourceURL constructs the well-known OAuth Protected Resource metadata URL.
// Per RFC 9728, the well-known suffix is inserted between the host and the path.
// e.g. https://example.com/path → https://example.com/.well-known/oauth-protected-resource/path
func buildWellKnownResourceURL(baseURL string) string {
	return buildWellKnownSuffixURL(baseURL, "oauth-protected-resource")
}

// buildWellKnownSuffixURL inserts a /.well-known/<suffix> between the host and path of a URL.
func buildWellKnownSuffixURL(baseURL, suffix string) string {
	u, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil {
		return baseURL + "/.well-known/" + suffix
	}
	return fmt.Sprintf("%s://%s/.well-known/%s%s", u.Scheme, u.Host, suffix, u.Path)
}

// fetchJSON fetches JSON from a URL and decodes it into the target.
func fetchJSON[T any](ctx context.Context, logger *slog.Logger, guardianPolicy *guardian.Policy, url string) (*T, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	client := guardianPolicy.Client(guardian.WithDefaultRetryConfig())
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer o11y.LogDefer(ctx, logger, func() error { return resp.Body.Close() })

	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		// A 4xx is the server deliberately answering that this well-known
		// document is not served — not-published, not a failed probe.
		return nil, fmt.Errorf("HTTP %d: %w", resp.StatusCode, errWellKnownAbsent)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	return &result, nil
}
