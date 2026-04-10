// Package oauthtest provides helpers for creating OAuth-configured toolsets in tests.
package oauthtest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauth"
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// ProxyToolsetResult holds the objects created by CreateProxyToolset.
type ProxyToolsetResult struct {
	Toolset       toolsets_repo.Toolset
	ProxyServer   oauth_repo.OauthProxyServer
	ProxyProvider oauth_repo.OauthProxyProvider
}

// ProxyToolsetOpts configures CreateProxyToolset.
type ProxyToolsetOpts struct {
	// Slug prefix for the toolset. A UUID suffix is appended automatically.
	Slug string
	// IsPublic sets McpIsPublic on the toolset. Default false (private).
	IsPublic bool
	// ProviderType is "gram" or "custom". Defaults to "gram".
	ProviderType string
}

// CreateProxyToolset creates a toolset linked to an OAuth proxy server and provider.
func CreateProxyToolset(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	authCtx *contextvalues.AuthContext,
	opts ProxyToolsetOpts,
) ProxyToolsetResult {
	t.Helper()

	suffix := uuid.New().String()[:8]
	if opts.ProviderType == "" {
		opts.ProviderType = string(oauth.OAuthProxyProviderTypeGram)
	}
	if opts.Slug == "" {
		opts.Slug = "oauth-proxy"
	}
	slug := opts.Slug + "-" + suffix

	oauthRepo := oauth_repo.New(conn)
	toolsetsRepo := toolsets_repo.New(conn)

	proxyServer, err := oauthRepo.UpsertOAuthProxyServer(ctx, oauth_repo.UpsertOAuthProxyServerParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      "oauth-server-" + suffix,
		Audience:  pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	proxyProvider, err := oauthRepo.UpsertOAuthProxyProvider(ctx, oauth_repo.UpsertOAuthProxyProviderParams{
		ProjectID:                         *authCtx.ProjectID,
		OauthProxyServerID:                proxyServer.ID,
		Slug:                              "oauth-provider-" + suffix,
		ProviderType:                      opts.ProviderType,
		ScopesSupported:                   []string{},
		ResponseTypesSupported:            []string{},
		ResponseModesSupported:            []string{},
		GrantTypesSupported:               []string{},
		TokenEndpointAuthMethodsSupported: []string{},
		SecurityKeyNames:                  []string{},
		Secrets:                           []byte("{}"),
	})
	require.NoError(t, err)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "OAuth Proxy MCP " + suffix,
		Slug:                   slug,
		Description:            conv.ToPGText("Test toolset with OAuth proxy"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	if opts.IsPublic {
		toolset, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
			Name:                   toolset.Name,
			Description:            toolset.Description,
			DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
			McpSlug:                toolset.McpSlug,
			McpIsPublic:            true,
			McpEnabled:             toolset.McpEnabled,
			CustomDomainID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			ToolSelectionMode:      "",
			Slug:                   toolset.Slug,
			ProjectID:              toolset.ProjectID,
		})
		require.NoError(t, err)
	}

	toolset, err = toolsetsRepo.UpdateToolsetOAuthProxyServer(ctx, toolsets_repo.UpdateToolsetOAuthProxyServerParams{
		OauthProxyServerID: uuid.NullUUID{UUID: proxyServer.ID, Valid: true},
		Slug:               toolset.Slug,
		ProjectID:          toolset.ProjectID,
	})
	require.NoError(t, err)

	return ProxyToolsetResult{
		Toolset:       toolset,
		ProxyServer:   proxyServer,
		ProxyProvider: proxyProvider,
	}
}

// ExternalOAuthToolsetResult holds the objects created by CreateExternalOAuthToolset.
type ExternalOAuthToolsetResult struct {
	Toolset        toolsets_repo.Toolset
	ServerMetadata oauth_repo.ExternalOauthServerMetadatum
}

// ExternalOAuthToolsetOpts configures CreateExternalOAuthToolset.
type ExternalOAuthToolsetOpts struct {
	// Slug prefix for the toolset. A UUID suffix is appended automatically.
	Slug string
	// IsPublic sets McpIsPublic on the toolset. Default false (private).
	IsPublic bool
	// Metadata is RFC 8414 compliant JSON. If nil, a minimal default is used.
	// Ignored when AuthServer is set (the server's metadata is used instead).
	Metadata []byte
	// AuthServer, when set, wires the toolset metadata to a live
	// AuthorizationServer so that the external OAuth endpoints are addressable.
	AuthServer *AuthorizationServer
}

// CreateExternalOAuthToolset creates a toolset linked to an external OAuth server.
func CreateExternalOAuthToolset(
	t *testing.T,
	ctx context.Context,
	conn *pgxpool.Pool,
	authCtx *contextvalues.AuthContext,
	opts ExternalOAuthToolsetOpts,
) ExternalOAuthToolsetResult {
	t.Helper()

	suffix := uuid.New().String()[:8]
	if opts.Slug == "" {
		opts.Slug = "oauth-external"
	}
	slug := opts.Slug + "-" + suffix

	if opts.AuthServer != nil {
		opts.Metadata = opts.AuthServer.Metadata()
	} else if opts.Metadata == nil {
		meta := map[string]any{
			"issuer":                   "https://test-oauth-server.example.com",
			"authorization_endpoint":   "https://test-oauth-server.example.com/authorize",
			"token_endpoint":           "https://test-oauth-server.example.com/token",
			"response_types_supported": []string{"code"},
			"grant_types_supported":    []string{"authorization_code"},
		}
		var err error
		opts.Metadata, err = json.Marshal(meta)
		require.NoError(t, err)
	}

	oauthRepo := oauth_repo.New(conn)
	toolsetsRepo := toolsets_repo.New(conn)

	serverMetadata, err := oauthRepo.CreateExternalOAuthServerMetadata(ctx, oauth_repo.CreateExternalOAuthServerMetadataParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      "external-oauth-" + suffix,
		Metadata:  opts.Metadata,
	})
	require.NoError(t, err)

	toolset, err := toolsetsRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "External OAuth MCP " + suffix,
		Slug:                   slug,
		Description:            conv.ToPGText("Test toolset with external OAuth"),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                conv.ToPGText(slug),
		McpEnabled:             true,
	})
	require.NoError(t, err)

	if opts.IsPublic {
		toolset, err = toolsetsRepo.UpdateToolset(ctx, toolsets_repo.UpdateToolsetParams{
			Name:                   toolset.Name,
			Description:            toolset.Description,
			DefaultEnvironmentSlug: toolset.DefaultEnvironmentSlug,
			McpSlug:                toolset.McpSlug,
			McpIsPublic:            true,
			McpEnabled:             toolset.McpEnabled,
			CustomDomainID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			ToolSelectionMode:      "",
			Slug:                   toolset.Slug,
			ProjectID:              toolset.ProjectID,
		})
		require.NoError(t, err)
	}

	toolset, err = toolsetsRepo.UpdateToolsetExternalOAuthServer(ctx, toolsets_repo.UpdateToolsetExternalOAuthServerParams{
		ExternalOauthServerID: uuid.NullUUID{UUID: serverMetadata.ID, Valid: true},
		Slug:                  toolset.Slug,
		ProjectID:             toolset.ProjectID,
	})
	require.NoError(t, err)

	return ExternalOAuthToolsetResult{
		Toolset:        toolset,
		ServerMetadata: serverMetadata,
	}
}
