package externalmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
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
	Issuer                string // Authorization server issuer from RFC 8414 metadata.
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
func DiscoverOAuthMetadata(ctx context.Context, logger *slog.Logger, guardianPolicy *guardian.Policy, wwwAuthenticate string, remoteURL string) (*OAuthDiscoveryResult, error) {
	// Parse the WWW-Authenticate header
	params := parseWWWAuthenticate(wwwAuthenticate)

	var resourceMeta *protectedResourceMetadata
	var authServerMeta *authServerMetadata

	// Strategy 1: Check for auth_server_metadata in header (direct AS metadata URL)
	if asURL, ok := params["auth_server_metadata"]; ok && asURL != "" {
		meta, err := fetchAuthServerMetadata(ctx, logger, guardianPolicy, asURL, "")
		if err != nil {
			return nil, fmt.Errorf("fetch advertised authorization server metadata: %w", err)
		}
		authServerMeta = meta
	}

	// Strategy 2: Check for resource_metadata in header (Protected Resource metadata)
	if rmURL, ok := params["resource_metadata"]; ok && rmURL != "" {
		meta, err := fetchJSON[protectedResourceMetadata](ctx, logger, guardianPolicy, rmURL)
		if err == nil && meta != nil {
			resourceMeta = meta
			// Follow the chain to get AS metadata
			if len(meta.AuthorizationServers) > 0 {
				asMeta, err := fetchAdvertisedAuthServerMetadata(ctx, logger, guardianPolicy, meta.AuthorizationServers)
				if err != nil {
					return nil, fmt.Errorf("fetch protected resource authorization server metadata: %w", err)
				}
				authServerMeta = asMeta
			}
		}
	}

	// Strategy 3: Probe well-known locations derived from the remote URL
	if authServerMeta == nil {
		// Try OAuth Protected Resource metadata first
		prURL := buildWellKnownResourceURL(remoteURL)
		meta, err := fetchJSON[protectedResourceMetadata](ctx, logger, guardianPolicy, prURL)
		if err == nil && meta != nil {
			resourceMeta = meta
			// Follow the chain
			if len(meta.AuthorizationServers) > 0 {
				asMeta, err := fetchAdvertisedAuthServerMetadata(ctx, logger, guardianPolicy, meta.AuthorizationServers)
				if err != nil {
					return nil, fmt.Errorf("fetch discovered authorization server metadata: %w", err)
				}
				authServerMeta = asMeta
			}
		}

		// Try OAuth Authorization Server metadata directly
		if authServerMeta == nil {
			asURL := buildWellKnownURL(remoteURL)
			asMeta, _ := fetchAuthServerMetadata(ctx, logger, guardianPolicy, asURL, issuerFromWellKnownBase(remoteURL))
			if asMeta != nil {
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

			prURL := buildWellKnownResourceURL(rootURL)
			meta, err := fetchJSON[protectedResourceMetadata](ctx, logger, guardianPolicy, prURL)
			if err == nil && meta != nil {
				resourceMeta = meta
				if len(meta.AuthorizationServers) > 0 {
					asMeta, err := fetchAdvertisedAuthServerMetadata(ctx, logger, guardianPolicy, meta.AuthorizationServers)
					if err != nil {
						return nil, fmt.Errorf("fetch origin authorization server metadata: %w", err)
					}
					authServerMeta = asMeta
				}
			}

			if authServerMeta == nil {
				asURL := buildWellKnownURL(rootURL)
				asMeta, _ := fetchAuthServerMetadata(ctx, logger, guardianPolicy, asURL, rootURL)
				if asMeta != nil {
					authServerMeta = asMeta
				}
			}
		}
	}

	// Determine the OAuth version based on what we found
	result := &OAuthDiscoveryResult{
		Version:               OAuthVersionNone,
		Issuer:                "",
		AuthorizationEndpoint: "",
		TokenEndpoint:         "",
		RegistrationEndpoint:  "",
		ScopesSupported:       nil,
	}

	if authServerMeta != nil {
		result.Issuer = authServerMeta.Issuer
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

func issuerFromWellKnownBase(baseURL string) string {
	u, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, u.Path)
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

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	return &result, nil
}

func fetchAuthServerMetadata(
	ctx context.Context,
	logger *slog.Logger,
	guardianPolicy *guardian.Policy,
	metadataURL string,
	expectedIssuer string,
) (*authServerMetadata, error) {
	metadata, err := fetchJSON[authServerMetadata](ctx, logger, guardianPolicy, metadataURL)
	if err != nil {
		return nil, err
	}
	if metadata.Issuer == "" {
		return nil, fmt.Errorf("authorization server metadata missing issuer")
	}
	if err := validateOAuthIssuer(metadata.Issuer); err != nil {
		return nil, fmt.Errorf("invalid authorization server issuer: %w", err)
	}
	for name, endpoint := range map[string]string{
		"authorization_endpoint": metadata.AuthorizationEndpoint,
		"token_endpoint":         metadata.TokenEndpoint,
		"registration_endpoint":  metadata.RegistrationEndpoint,
	} {
		if endpoint != "" {
			if err := validateOAuthEndpoint(endpoint); err != nil {
				return nil, fmt.Errorf("invalid %s: %w", name, err)
			}
		}
	}
	if expectedIssuer == "" {
		if buildWellKnownURL(metadata.Issuer) != metadataURL {
			return nil, fmt.Errorf("authorization server metadata issuer does not match metadata URL")
		}
		return metadata, nil
	}
	if metadata.Issuer != expectedIssuer {
		return nil, fmt.Errorf("authorization server metadata issuer mismatch")
	}
	return metadata, nil
}

func fetchAdvertisedAuthServerMetadata(
	ctx context.Context,
	logger *slog.Logger,
	guardianPolicy *guardian.Policy,
	issuers []string,
) (*authServerMetadata, error) {
	var errs []error
	for _, issuer := range issuers {
		metadata, err := fetchAuthServerMetadata(ctx, logger, guardianPolicy, buildWellKnownURL(issuer), issuer)
		if err == nil {
			return metadata, nil
		}
		errs = append(errs, fmt.Errorf("issuer %q: %w", issuer, err))
	}

	return nil, fmt.Errorf("no advertised authorization server metadata was usable: %w", errors.Join(errs...))
}

func validateOAuthIssuer(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse issuer URL: %w", err)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("must not contain a query or fragment")
	}
	return validateSecureOAuthURL(u)
}

func validateOAuthEndpoint(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse endpoint URL: %w", err)
	}
	if u.Fragment != "" {
		return fmt.Errorf("must not contain a fragment")
	}
	return validateSecureOAuthURL(u)
}

func validateSecureOAuthURL(u *url.URL) error {
	if u.Host == "" || u.User != nil {
		return fmt.Errorf("must be an absolute URL without userinfo")
	}
	if u.Scheme == "https" {
		return nil
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if u.Scheme == "http" && (host == "localhost" || ip != nil && ip.IsLoopback()) {
		return nil
	}
	return fmt.Errorf("must use HTTPS")
}
