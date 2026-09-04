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
	oauth_repo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

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
	// Callers that want to wire the toolset to a live upstream OAuth server
	// (e.g. dev-idp via devidptest) should pass the bytes returned by the
	// server's metadata helper here (e.g. inst.OAuth21Metadata(t)).
	Metadata []byte
	// AuthorizationServerIssuer configures provider-hosted discovery. When set,
	// Metadata must be nil to preserve the database source XOR.
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

	var err error
	if opts.Metadata == nil && opts.AuthorizationServerIssuer == nil {
		meta := map[string]any{
			"issuer":                   "https://test-oauth-server.example.com",
			"authorization_endpoint":   "https://test-oauth-server.example.com/authorize",
			"token_endpoint":           "https://test-oauth-server.example.com/token",
			"response_types_supported": []string{"code"},
			"grant_types_supported":    []string{"authorization_code"},
		}
		opts.Metadata, err = json.Marshal(meta)
		require.NoError(t, err)
	}

	oauthRepo := oauth_repo.New(conn)
	toolsetsRepo := toolsets_repo.New(conn)

	var serverMetadata oauth_repo.ExternalOauthServerMetadatum
	if opts.AuthorizationServerIssuer == nil {
		serverMetadata, err = oauthRepo.CreateExternalOAuthServerMetadata(ctx, oauth_repo.CreateExternalOAuthServerMetadataParams{
			ProjectID: *authCtx.ProjectID,
			Slug:      "external-oauth-" + suffix,
			Metadata:  opts.Metadata,
		})
	} else {
		err = conn.QueryRow(ctx, `
			INSERT INTO external_oauth_server_metadata (project_id, slug, authorization_server_issuer)
			VALUES ($1, $2, $3)
			RETURNING id, project_id, slug, metadata, authorization_server_issuer, created_at, updated_at, deleted_at, deleted
		`, *authCtx.ProjectID, "external-oauth-"+suffix, *opts.AuthorizationServerIssuer).Scan(
			&serverMetadata.ID, &serverMetadata.ProjectID, &serverMetadata.Slug, &serverMetadata.Metadata,
			&serverMetadata.AuthorizationServerIssuer, &serverMetadata.CreatedAt, &serverMetadata.UpdatedAt,
			&serverMetadata.DeletedAt, &serverMetadata.Deleted,
		)
	}
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
