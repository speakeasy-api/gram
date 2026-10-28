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
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// ExternalOAuthToolsetResult holds the objects created by CreateExternalOAuthToolset.
type ExternalOAuthToolsetResult struct {
	Toolset toolsets_repo.Toolset
	// McpSlug is the toolset's legacy /mcp/<slug> alias, which serves it.
	McpSlug        string
	ServerMetadata oauth_repo.ExternalOauthServerMetadatum
}

// ExternalOAuthToolsetOpts configures CreateExternalOAuthToolset.
type ExternalOAuthToolsetOpts struct {
	// Slug prefix for the toolset. A UUID suffix is appended automatically.
	Slug string
	// IsPublic makes the canonical server public. Default false (private).
	IsPublic bool
	// Metadata is RFC 8414 compliant JSON. If nil, a minimal default is used.
	// Callers that want to wire the toolset to a live upstream OAuth server
	// (e.g. dev-idp via devidptest) should pass the bytes returned by the
	// server's metadata helper here (e.g. inst.OAuth21Metadata(t)).
	Metadata []byte
	// AuthorizationServerIssuer is the optional provider-hosted issuer. When
	// set, Metadata must be nil to preserve the database source XOR.
	AuthorizationServerIssuer *string
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

	if opts.Metadata == nil && opts.AuthorizationServerIssuer == nil {
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

	authorizationServerIssuer := pgtype.Text{}
	if opts.AuthorizationServerIssuer != nil {
		authorizationServerIssuer = conv.ToPGText(*opts.AuthorizationServerIssuer)
	}

	serverMetadata, err := oauthRepo.CreateExternalOAuthServerMetadata(ctx, oauth_repo.CreateExternalOAuthServerMetadataParams{
		ProjectID:                 *authCtx.ProjectID,
		Slug:                      "external-oauth-" + suffix,
		Metadata:                  opts.Metadata,
		AuthorizationServerIssuer: authorizationServerIssuer,
	})
	require.NoError(t, err)

	toolset := createLegacyHostedToolset(t, ctx, conn, authCtx, legacyHostedToolset{
		name: "External OAuth MCP " + suffix, slug: slug, description: "Test toolset with external OAuth",
		public: opts.IsPublic, issuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})

	toolset, err = toolsetsRepo.UpdateToolsetExternalOAuthServer(ctx, toolsets_repo.UpdateToolsetExternalOAuthServerParams{
		ExternalOauthServerID: uuid.NullUUID{UUID: serverMetadata.ID, Valid: true},
		Slug:                  toolset.Slug,
		ProjectID:             toolset.ProjectID,
	})
	require.NoError(t, err)

	return ExternalOAuthToolsetResult{
		Toolset:        toolset,
		McpSlug:        slug,
		ServerMetadata: serverMetadata,
	}
}

// legacyHostedToolset is a toolset served through its legacy /mcp/<slug> alias, as toolsets created before hosted
// servers owned their endpoints are.
type legacyHostedToolset struct {
	name        string
	slug        string
	description string
	public      bool
	issuerID    uuid.NullUUID
}

// createLegacyHostedToolset creates the toolset, its alias, and an enabled canonical MCP server without endpoints, so
// requests reach it through the legacy slug fallback.
func createLegacyHostedToolset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, authCtx *contextvalues.AuthContext, input legacyHostedToolset) toolsets_repo.Toolset {
	t.Helper()

	toolset, err := toolsets_repo.New(conn).CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   input.name,
		Slug:                   input.slug,
		Description:            conv.ToPGText(input.description),
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	testenv.SetLegacyAlias(t, ctx, conn, toolset.ProjectID, toolset.ID, input.slug, uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	vis := visibility.Private
	if input.public {
		vis = visibility.Public
	}
	_, err = mcpservers_repo.New(conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                    toolset.ID,
		ProjectID:             toolset.ProjectID,
		Name:                  conv.ToPGText(toolset.Name),
		Slug:                  pgtype.Text{String: "", Valid: false},
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   input.issuerID,
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            vis,
		NetworkAccessMode:     pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	toolset, err = toolsets_repo.New(conn).GetToolsetByIDAndProject(ctx, toolsets_repo.GetToolsetByIDAndProjectParams{ID: toolset.ID, ProjectID: toolset.ProjectID})
	require.NoError(t, err)
	return toolset
}
