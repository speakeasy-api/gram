package testenv

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/hostedmcp/legacyalias"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// HostedMCP is the hosting state a test gives a toolset's canonical MCP server.
type HostedMCP struct {
	// Visibility is public, private or disabled.
	Visibility string
	// UserSessionIssuerID, when set, replaces the server's issuer; an invalid value clears it.
	UserSessionIssuerID *uuid.NullUUID
	// Slug, when set, replaces the server's endpoints with one at this slug.
	Slug string
	// CustomDomainID puts that endpoint on a custom domain; invalid means the platform domain.
	CustomDomainID uuid.NullUUID
}

// SetHostedMCP writes hosting state onto the canonical MCP server of a toolset created through the toolsets service.
func SetHostedMCP(t *testing.T, ctx context.Context, db testrepo.DBTX, projectID, toolsetID uuid.UUID, hosted HostedMCP) {
	t.Helper()

	q := testrepo.New(db)
	if hosted.Slug != "" {
		require.NoError(t, q.RetireHostedToolsetEndpointsFixture(ctx, testrepo.RetireHostedToolsetEndpointsFixtureParams{ID: uuid.NullUUID{UUID: toolsetID, Valid: true}, ProjectID: projectID}))
		rows, err := q.CreateHostedToolsetEndpointFixture(ctx, testrepo.CreateHostedToolsetEndpointFixtureParams{
			ID: toolsetID, ProjectID: projectID, CustomDomainID: hosted.CustomDomainID, Slug: hosted.Slug,
		})
		require.NoError(t, err)
		require.Equal(t, int64(1), rows, "toolset has no canonical MCP server")
	}
	if hosted.UserSessionIssuerID != nil {
		rows, err := q.SetHostedToolsetIssuerFixture(ctx, testrepo.SetHostedToolsetIssuerFixtureParams{ID: toolsetID, ProjectID: projectID, UserSessionIssuerID: *hosted.UserSessionIssuerID})
		require.NoError(t, err)
		require.Equal(t, int64(1), rows, "toolset has no canonical MCP server")
	}
	if hosted.Visibility != "" {
		rows, err := q.SetHostedToolsetVisibilityFixture(ctx, testrepo.SetHostedToolsetVisibilityFixtureParams{ID: toolsetID, ProjectID: projectID, Visibility: hosted.Visibility})
		require.NoError(t, err)
		require.Equal(t, int64(1), rows, "toolset has no canonical MCP server")
	}
}

// SetLegacyAlias sets the legacy /mcp/<slug> alias of a toolset; an empty slug clears it.
func SetLegacyAlias(t *testing.T, ctx context.Context, db testrepo.DBTX, projectID, toolsetID uuid.UUID, slug string, customDomainID uuid.NullUUID) {
	t.Helper()

	require.NoError(t, legacyalias.SetFixture(ctx, db, projectID, toolsetID, legacyalias.Alias{
		Slug: pgtype.Text{String: slug, Valid: slug != ""}, CustomDomainID: customDomainID,
	}))
}
