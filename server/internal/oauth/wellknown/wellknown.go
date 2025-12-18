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
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type OAuthServerMetadata struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	RegistrationEndpoint          string   `json:"registration_endpoint"`
	ResponseTypesSupported        []string `json:"response_types_supported"`
	GrantTypesSupported           []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
}

type OAuthServerMetadataResultKind string

const (
	OAuthServerMetadataResultKindStatic OAuthServerMetadataResultKind = "static"
	OAuthServerMetadataResultKindProxy  OAuthServerMetadataResultKind = "proxy"
)

type OAuthServerMetadataResult struct {
	Kind     OAuthServerMetadataResultKind
	Static   *OAuthServerMetadata
	ProxyURL string
}

type OAuthRepo interface {
	GetExternalOAuthServerMetadata(ctx context.Context, arg repo.GetExternalOAuthServerMetadataParams) (repo.ExternalOauthServerMetadatum, error)
}

func ResolveOAuthServerMetadataFromToolset(
	ctx context.Context,
	logger *slog.Logger,
	db mv.DBTX,
	oauthRepo OAuthRepo,
	toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents],
	toolset *toolsets_repo.Toolset,
	baseURL string,
	mcpSlug string,
) (*OAuthServerMetadataResult, error) {
	if toolset.OauthProxyServerID.Valid {
		return &OAuthServerMetadataResult{
			Kind: OAuthServerMetadataResultKindStatic,
			Static: &OAuthServerMetadata{
				Issuer:                        baseURL + "/oauth/" + mcpSlug,
				AuthorizationEndpoint:         baseURL + "/oauth/" + mcpSlug + "/authorize",
				TokenEndpoint:                 baseURL + "/oauth/" + mcpSlug + "/token",
				RegistrationEndpoint:          baseURL + "/oauth/" + mcpSlug + "/register",
				ResponseTypesSupported:        []string{"code"},
				GrantTypesSupported:           []string{"authorization_code"},
				CodeChallengeMethodsSupported: []string{"plain", "S256"},
			},
			ProxyURL: "",
		}, nil
	}

	if toolset.ExternalOauthServerID.Valid {
		externalOAuthServer, err := oauthRepo.GetExternalOAuthServerMetadata(ctx, repo.GetExternalOAuthServerMetadataParams{
			ProjectID: toolset.ProjectID,
			ID:        toolset.ExternalOauthServerID.UUID,
		})
		if err != nil {
			return nil, fmt.Errorf("get external oauth server metadata: %w", err)
		}

		var metadata OAuthServerMetadata
		if err := json.Unmarshal(externalOAuthServer.Metadata, &metadata); err != nil {
			return nil, fmt.Errorf("unmarshal oauth server metadata: %w", err)
		}

		return &OAuthServerMetadataResult{
			Kind:     OAuthServerMetadataResultKindStatic,
			Static:   &metadata,
			ProxyURL: "",
		}, nil
	}

	fullToolset, err := mv.DescribeToolset(ctx, logger, db, mv.ProjectID(toolset.ProjectID), mv.ToolsetSlug(toolset.Slug), toolsetCache)
	if err != nil {
		return nil, err
	}

	if oauthConfig := externalmcp.ResolveOAuthConfig(fullToolset); oauthConfig != nil {
		return &OAuthServerMetadataResult{
			Kind:     OAuthServerMetadataResultKindProxy,
			Static:   nil,
			ProxyURL: oauthConfig.WellKnownOAuthServerURL(),
		}, nil
	}

	return nil, nil
}

func IsToolsetOAuthProtected(
	ctx context.Context,
	logger *slog.Logger,
	db mv.DBTX,
	toolsetCache *cache.TypedCacheObject[mv.ToolsetBaseContents],
	toolset *toolsets_repo.Toolset,
) (bool, error) {
	if toolset.OauthProxyServerID.Valid || toolset.ExternalOauthServerID.Valid {
		return true, nil
	}

	fullToolset, err := mv.DescribeToolset(ctx, logger, db, mv.ProjectID(toolset.ProjectID), mv.ToolsetSlug(toolset.Slug), toolsetCache)
	if err != nil {
		return false, err
	}

	return externalmcp.ResolveOAuthConfig(fullToolset) != nil, nil
}
