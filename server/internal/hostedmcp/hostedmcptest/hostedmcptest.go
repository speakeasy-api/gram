// Package hostedmcptest checks that a hosted toolset's columns mirror its canonical server, so pods that still read
// the columns and run the toolset-to-server sync see one state and that sync writes nothing.
package hostedmcptest

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type hostedState struct {
	toolset   toolsetsrepo.Toolset
	server    mcpserversrepo.McpServer
	endpoints []mcpendpointsrepo.McpEndpoint
}

func load(t *testing.T, ctx context.Context, db toolsetsrepo.DBTX, projectID, toolsetID uuid.UUID) hostedState {
	t.Helper()
	toolset, err := toolsetsrepo.New(db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: toolsetID, ProjectID: projectID})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: toolsetID, ProjectID: projectID})
	require.NoError(t, err)
	// The same live-domain endpoint set MirrorToToolset reads.
	rows, err := mcpendpointsrepo.New(db).ListMCPEndpointsOnLiveDomainsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsOnLiveDomainsByMCPServerIDParams{
		OrganizationID: toolset.OrganizationID, ProjectID: projectID, McpServerID: toolsetID,
	})
	require.NoError(t, err)
	endpoints := make([]mcpendpointsrepo.McpEndpoint, 0, len(rows))
	for _, row := range rows {
		endpoints = append(endpoints, row.McpEndpoint)
	}
	return hostedState{toolset: toolset, server: server, endpoints: endpoints}
}

// oldVisibility is the toolset-column visibility mapping older pods compare the canonical server against.
func oldVisibility(toolset toolsetsrepo.Toolset) string {
	switch {
	case !toolset.McpEnabled:
		return visibility.Disabled
	case toolset.McpIsPublic:
		return visibility.Public
	default:
		return visibility.Private
	}
}

// RequireMirrored asserts the toolset's hosting columns mirror its canonical server and single endpoint, that an
// older pod's drift check sees them in step, and that the older toolset-to-server sync writes nothing.
func RequireMirrored(t *testing.T, ctx context.Context, db *pgxpool.Pool, projectID, toolsetID uuid.UUID) {
	t.Helper()
	requireMirrored(t, ctx, db, projectID, toolsetID, 1)
}

// RequireMirroredWithoutAddress is RequireMirrored for a hosted server left with no endpoint, as a deleted domain
// leaves it: the toolset keeps its last address and the older sync must not restore it.
func RequireMirroredWithoutAddress(t *testing.T, ctx context.Context, db *pgxpool.Pool, projectID, toolsetID uuid.UUID) {
	t.Helper()
	requireMirrored(t, ctx, db, projectID, toolsetID, 0)
}

func requireMirrored(t *testing.T, ctx context.Context, db *pgxpool.Pool, projectID, toolsetID uuid.UUID, endpoints int) {
	t.Helper()

	state := load(t, ctx, db, projectID, toolsetID)
	// Expected columns come from the rows directly, not from the mirror under test.
	if state.server.Name.Valid && state.server.Name.String != "" {
		require.Equal(t, state.server.Name.String, state.toolset.Name, "name")
	}
	require.Equal(t, state.server.Visibility != visibility.Disabled, state.toolset.McpEnabled, "mcp_enabled")
	if state.server.Visibility != visibility.Disabled {
		require.Equal(t, state.server.Visibility == visibility.Public, state.toolset.McpIsPublic, "mcp_is_public")
	}
	require.Equal(t, state.server.UserSessionIssuerID, state.toolset.UserSessionIssuerID, "user_session_issuer_id")
	require.Equal(t, state.server.ToolVariationsGroupID, state.toolset.ToolVariationsGroupID, "tool_variations_group_id")
	if len(state.endpoints) == 1 {
		require.Equal(t, state.endpoints[0].Slug, state.toolset.McpSlug.String, "mcp_slug")
		require.Equal(t, state.endpoints[0].CustomDomainID, state.toolset.CustomDomainID, "custom_domain_id")
	}
	require.Equal(t, oldVisibility(state.toolset), state.server.Visibility, "an older pod's drift check sees the server in step")
	require.Equal(t, state.toolset.McpSlug, state.server.Slug, "the server slug follows its single address")
	require.Len(t, state.endpoints, endpoints, "a hosted server keeps at most a single address")

	tx := testenv.BeginTx(t, ctx, db)
	defer func() { _ = tx.Rollback(context.Background()) }()
	locked, err := toolsetsrepo.New(tx).GetToolsetForUpdate(ctx, toolsetsrepo.GetToolsetForUpdateParams{Slug: state.toolset.Slug, ProjectID: projectID})
	require.NoError(t, err)
	auditsBefore, err := audittest.AuditLogCount(ctx, tx)
	require.NoError(t, err)
	_, err = hostedmcp.BackfillSync(ctx, tx, audit.NewLogger(), hostedmcp.SystemActor("old-pod-sync"), locked)
	require.NoError(t, err)
	auditsAfter, err := audittest.AuditLogCount(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, auditsBefore, auditsAfter, "the older sync audits nothing")
	after := load(t, ctx, tx, projectID, toolsetID)
	require.Equal(t, state.server, after.server, "the older sync leaves the server alone")
	require.Equal(t, state.endpoints, after.endpoints, "the older sync leaves the endpoint alone")
	require.Equal(t, state.toolset, after.toolset, "the older sync leaves the toolset alone")
}
