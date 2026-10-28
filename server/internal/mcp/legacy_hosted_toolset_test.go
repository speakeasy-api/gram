package mcp_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// hostedToolset is a toolset with the hosting state its test fixture gave it.
type hostedToolset struct {
	toolsets_repo.Toolset
	McpSlug             pgtype.Text
	CustomDomainID      uuid.NullUUID
	UserSessionIssuerID uuid.NullUUID
}

// hostLegacyToolset gives a toolset its legacy alias at slug and a canonical MCP server without endpoints, so it is
// served through the legacy slug fallback. The server gets no slug of its own, so tests can reuse slug for other servers.
func hostLegacyToolset(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, slug, visibility string, issuerID uuid.NullUUID) hostedToolset {
	t.Helper()

	testenv.SetLegacyAlias(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, slug, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	_, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    toolset.ID,
		ProjectID:             toolset.ProjectID,
		Name:                  pgtype.Text{String: toolset.Name, Valid: true},
		Slug:                  pgtype.Text{String: "", Valid: false},
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   issuerID,
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            visibility,
		NetworkAccessMode:     pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	toolset, err = toolsets_repo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsets_repo.GetToolsetByIDAndProjectParams{ID: toolset.ID, ProjectID: toolset.ProjectID})
	require.NoError(t, err)
	return hostedToolset{Toolset: toolset, McpSlug: pgtype.Text{String: slug, Valid: true}, CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, UserSessionIssuerID: issuerID}
}

// createUnhostedToolset creates a toolset with no canonical MCP server, which only internal callers reach.
func createUnhostedToolset(t *testing.T, ctx context.Context, ti *testInstance, authCtx *contextvalues.AuthContext, slug string) toolsets_repo.Toolset {
	t.Helper()

	toolset, err := toolsets_repo.New(ti.conn).CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Unhosted " + slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return toolset
}

// hostedEndpointServer is the server a hosted endpoint test calls through: a separate wrapper at slug, or with legacy
// the canonical server exposed at another slug so slug reaches it through the legacy fallback.
func hostedEndpointServer(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, slug string, legacy bool) mcpserversrepo.McpServer {
	t.Helper()

	if legacy {
		return exposeCanonicalWrapper(t, ctx, ti, toolset.ProjectID, toolset.ID, slug+"-wrapper", "public")
	}
	return createToolsetMcpEndpoint(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{UUID: uuid.Nil, Valid: false}, uuid.Nil)
}

// exposeCanonicalWrapper gives the toolset's canonical MCP server one platform endpoint at slug and returns the server.
func exposeCanonicalWrapper(t *testing.T, ctx context.Context, ti *testInstance, projectID, toolsetID uuid.UUID, slug, visibility string) mcpserversrepo.McpServer {
	t.Helper()

	testenv.SetHostedMCP(t, ctx, ti.conn, projectID, toolsetID, testenv.HostedMCP{Visibility: visibility, Slug: slug, CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}})
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: toolsetID, ProjectID: projectID})
	require.NoError(t, err)
	return server
}

// setToolsetIssuer gates the toolset's canonical MCP server with issuerID and returns the toolset carrying it.
func setToolsetIssuer(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, toolset hostedToolset, issuerID uuid.UUID) hostedToolset {
	t.Helper()

	issuer := uuid.NullUUID{UUID: issuerID, Valid: true}
	testenv.SetHostedMCP(t, ctx, conn, projectID, toolset.ID, testenv.HostedMCP{UserSessionIssuerID: &issuer})
	toolset.UserSessionIssuerID = issuer
	return toolset
}

func noIssuer() uuid.NullUUID { return uuid.NullUUID{UUID: uuid.Nil, Valid: false} }
