// Package wellknown resolves OAuth 2.1 well-known metadata for toolsets.
//
// It provides two capabilities:
//   - Determining whether a toolset is OAuth-protected (for oauth-protected-resource)
//   - Resolving OAuth server metadata (for oauth-authorization-server)
//
// Caveats:
//
// This implementation is tightly coupled to the MCP client authentication flow.
// The package's concerns are more broadly useful within Gram, but this revision
// only addresses the immediate client requirements rather than fully describing
// toolset authentication state.
//
// The methods here rely on reading the full toolset model view because OAuth
// state is currently inferred from tool definitions. Eventually, we'd prefer
// explicit user-assigned OAuth configuration on toolsets, and moving OAuth
// protectedness off of tools onto a separate abstraction. To mitigate the
// performance cost, we defer fetching the toolset model view until after
// exhausting other OAuth configuration sources.
package wellknown

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// OAuthAuthorizationServerPath is the well-known URI path for OAuth 2.0
// Authorization Server Metadata as defined by RFC 8414.
//
// https://datatracker.ietf.org/doc/html/rfc8414
const OAuthAuthorizationServerPath = "/.well-known/oauth-authorization-server"

// OAuthProtectedResourcePath is the well-known URI path for OAuth 2.0
// Protected Resource Metadata as defined by RFC 9728.
//
// https://datatracker.ietf.org/doc/html/rfc9728
const OAuthProtectedResourcePath = "/.well-known/oauth-protected-resource"

// OAuthProtectedResourceMetadata represents OAuth 2.0 Protected Resource Metadata (RFC 9728).
//
// Used for both serving Gram's own metadata documents and decoding metadata
// probed from upstream resource servers via [DiscoverProtectedResourceMetadata].
// Fields outside the minimum set required by the existing server-side callers
// are tagged omitempty so adding them does not change emitted documents.
type OAuthProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported,omitempty"`
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`
	ResourceDocumentation  string   `json:"resource_documentation,omitempty"`
}

// OAuthServerMetadata represents OAuth 2.0 Authorization Server Metadata (RFC 8414).
type OAuthServerMetadata struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	RegistrationEndpoint          string   `json:"registration_endpoint"`
	ScopesSupported               []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported        []string `json:"response_types_supported"`
	GrantTypesSupported           []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
}

type OAuthServerMetadataResultKind string

const (
	OAuthServerMetadataResultKindStatic OAuthServerMetadataResultKind = "static"
	OAuthServerMetadataResultKindRaw    OAuthServerMetadataResultKind = "raw"
	OAuthServerMetadataResultKindProxy  OAuthServerMetadataResultKind = "proxy"
)

type OAuthServerMetadataResult struct {
	Kind     OAuthServerMetadataResultKind
	Static   *OAuthServerMetadata
	Raw      json.RawMessage
	ProxyURL string
}

type OAuthRepo interface {
	GetExternalOAuthServerMetadata(ctx context.Context, arg repo.GetExternalOAuthServerMetadataParams) (repo.ExternalOauthServerMetadatum, error)
}

// ResolveOAuthServerMetadataFromToolset returns OAuth Authorization Server
// metadata for a toolset, or nil if the toolset is not OAuth-configured.
//
// oauthSlug is the slug used to address the Gram-hosted OAuth endpoints
// (`/oauth/{oauthSlug}/...`). Today the OAuth machinery is keyed by
// `toolsets.mcp_slug`, so callers should pass that value. The /x/mcp
// experimental endpoint uses the same OAuth flow under the hood, so it
// also passes `toolset.mcp_slug` here even though its protected-resource
// URL uses an `mcp_endpoints.slug` instead — see the companion
// resourceURL argument on [ResolveOAuthProtectedResourceFromToolset].
func ResolveOAuthServerMetadataFromToolset(
	ctx context.Context,
	logger *slog.Logger,
	db mv.DBTX,
	oauthRepo OAuthRepo,
	toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents],
	toolset *toolsets_repo.Toolset,
	baseURL string,
	oauthSlug string,
) (*OAuthServerMetadataResult, error) {
	if toolset.ExternalOauthServerID.Valid {
		externalOAuthServer, err := oauthRepo.GetExternalOAuthServerMetadata(ctx, repo.GetExternalOAuthServerMetadataParams{
			ProjectID: toolset.ProjectID,
			ID:        toolset.ExternalOauthServerID.UUID,
		})
		if err != nil {
			return nil, fmt.Errorf("get external oauth server metadata: %w", err)
		}

		return &OAuthServerMetadataResult{
			Kind:     OAuthServerMetadataResultKindRaw,
			Static:   nil,
			Raw:      externalOAuthServer.Metadata,
			ProxyURL: "",
		}, nil
	}

	return nil, nil
}

// ResolveOAuthProtectedResourceFromToolset returns OAuth Protected Resource
// Metadata for a toolset, or nil if the toolset is not OAuth-protected.
//
// resourceURL is the absolute URL of the protected resource (the runtime MCP
// endpoint). For /mcp callers this is `<baseURL>/mcp/<toolset.mcp_slug>`; for
// /x/mcp callers this is `<baseURL>/x/mcp/<mcp_endpoint.slug>`. It is used
// verbatim for both `resource` and `authorization_servers` so that the
// `/.well-known/...` discovery path on the protected resource resolves back
// to the Gram-hosted authorization server metadata.
func ResolveOAuthProtectedResourceFromToolset(
	ctx context.Context,
	logger *slog.Logger,
	db mv.DBTX,
	toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents],
	toolset *toolsets_repo.Toolset,
	resourceURL string,
) (*OAuthProtectedResourceMetadata, error) {
	// Check for external OAuth server configuration
	if toolset.ExternalOauthServerID.Valid {
		return &OAuthProtectedResourceMetadata{
			Resource:               resourceURL,
			AuthorizationServers:   []string{resourceURL},
			ScopesSupported:        nil,
			BearerMethodsSupported: nil,
			ResourceDocumentation:  "",
		}, nil
	}

	return nil, nil
}
