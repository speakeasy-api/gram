package externalmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
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
func DiscoverOAuthMetadata(ctx context.Context, logger *slog.Logger, wwwAuthenticate string, remoteURL string) (*OAuthDiscoveryResult, error) {
	// Parse the WWW-Authenticate header
	params := parseWWWAuthenticate(wwwAuthenticate)

	var resourceMeta *protectedResourceMetadata
	var authServerMeta *authServerMetadata

	// Strategy 1: Check for auth_server_metadata in header (direct AS metadata URL)
	if asURL, ok := params["auth_server_metadata"]; ok && asURL != "" {
		meta, err := fetchJSON[authServerMetadata](ctx, logger, asURL)
		if err == nil && meta != nil {
			authServerMeta = meta
		}
	}

	// Strategy 2: Check for resource_metadata in header (Protected Resource metadata)
	if rmURL, ok := params["resource_metadata"]; ok && rmURL != "" {
		meta, err := fetchJSON[protectedResourceMetadata](ctx, logger, rmURL)
		if err == nil && meta != nil {
			resourceMeta = meta
			// Follow the chain to get AS metadata
			if len(meta.AuthorizationServers) > 0 {
				asURL := buildWellKnownURL(meta.AuthorizationServers[0])
				asMeta, err := fetchJSON[authServerMetadata](ctx, logger, asURL)
				if err == nil && asMeta != nil {
					authServerMeta = asMeta
				}
			}
		}
	}

	// Strategy 3: Probe well-known locations at the server origin
	if authServerMeta == nil {
		origin, err := getOrigin(remoteURL)
		if err == nil {
			// Try OAuth Protected Resource metadata first
			prURL := origin + "/.well-known/oauth-protected-resource"
			meta, err := fetchJSON[protectedResourceMetadata](ctx, logger, prURL)
			if err == nil && meta != nil {
				resourceMeta = meta
				// Follow the chain
				if len(meta.AuthorizationServers) > 0 {
					asURL := buildWellKnownURL(meta.AuthorizationServers[0])
					asMeta, _ := fetchJSON[authServerMetadata](ctx, logger, asURL)
					if asMeta != nil {
						authServerMeta = asMeta
					}
				}
			}

			// Try OAuth Authorization Server metadata directly
			if authServerMeta == nil {
				asURL := origin + "/.well-known/oauth-authorization-server"
				asMeta, _ := fetchJSON[authServerMetadata](ctx, logger, asURL)
				if asMeta != nil {
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
	}

	if authServerMeta != nil {
		result.AuthorizationEndpoint = authServerMeta.AuthorizationEndpoint
		result.TokenEndpoint = authServerMeta.TokenEndpoint
		result.RegistrationEndpoint = authServerMeta.RegistrationEndpoint
		result.ScopesSupported = authServerMeta.ScopesSupported

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

// getOrigin extracts the origin (scheme + host) from a URL.
func getOrigin(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	return fmt.Sprintf("%s://%s", u.Scheme, u.Host), nil
}

// buildWellKnownURL constructs the well-known OAuth Authorization Server metadata URL.
func buildWellKnownURL(baseURL string) string {
	baseURL = strings.TrimSuffix(baseURL, "/")
	return baseURL + "/.well-known/oauth-authorization-server"
}

// fetchJSON fetches JSON from a URL and decodes it into the target.
func fetchJSON[T any](ctx context.Context, logger *slog.Logger, url string) (*T, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	client := retryablehttp.NewClient().StandardClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer o11y.LogDefer(ctx, logger, func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	return &result, nil
}
