package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestListMcpServers_ToolsetHostingServer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	newToolset := func(slug string) toolsetsrepo.Toolset {
		toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
			OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectID,
			Name: slug, Slug: slug,
		})
		require.NoError(t, err)
		return toolset
	}
	newServer := func(id uuid.UUID, toolset toolsetsrepo.Toolset, slug, visibility string, endpoint bool) uuid.UUID {
		server, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
			ID: id, ProjectID: projectID,
			Name: conv.ToPGText(slug), Slug: conv.ToPGText(slug),
			ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: visibility,
		})
		require.NoError(t, err)
		if endpoint {
			_, err = mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
				ProjectID:   projectID,
				McpServerID: uuid.NullUUID{UUID: server.ID, Valid: true},
				Slug:        slug + "-endpoint",
			})
			require.NoError(t, err)
		}
		return server.ID
	}

	// The canonical wrapper hosts its toolset even without an endpoint.
	canonicalToolset := newToolset("hosting-canonical")
	canonical := newServer(canonicalToolset.ID, canonicalToolset, "hosting-canonical", "private", false)
	beside := newServer(uuid.New(), canonicalToolset, "hosting-beside", "public", true)

	// Without one, the server with an endpoint hosts it, not a member wrapper without one.
	soloToolset := newToolset("hosting-solo")
	member := newServer(uuid.New(), soloToolset, "hosting-member", "private", false)
	solo := newServer(uuid.New(), soloToolset, "hosting-solo", "public", true)

	// The oldest server with an endpoint hosts it, whatever its visibility.
	pairToolset := newToolset("hosting-pair")
	disabled := newServer(uuid.New(), pairToolset, "hosting-disabled", "disabled", true)
	enabled := newServer(uuid.New(), pairToolset, "hosting-enabled", "private", true)

	// A deleted canonical wrapper or endpoint does not count.
	tombstoneToolset := newToolset("hosting-tombstone")
	deletedCanonical := newServer(tombstoneToolset.ID, tombstoneToolset, "hosting-deleted-canonical", "private", true)
	_, err := mcpserversrepo.New(ti.conn).DeleteMCPServer(ctx, mcpserversrepo.DeleteMCPServerParams{ID: deletedCanonical, ProjectID: projectID})
	require.NoError(t, err)
	retired := newServer(uuid.New(), tombstoneToolset, "hosting-retired", "public", true)
	_, err = mcpendpointsrepo.New(ti.conn).SoftDeleteMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.SoftDeleteMCPEndpointsByMCPServerIDParams{McpServerID: retired, ProjectID: projectID})
	require.NoError(t, err)
	live := newServer(uuid.New(), tombstoneToolset, "hosting-live", "public", true)

	// A toolset whose only server has no endpoint has no host.
	memberOnlyToolset := newToolset("hosting-member-only")
	memberOnly := newServer(uuid.New(), memberOnlyToolset, "hosting-member-only", "private", false)

	result, err := ti.service.ListMcpServers(ctx, &gen.ListMcpServersPayload{})
	require.NoError(t, err)
	hosts := make(map[string]*string)
	for _, server := range result.McpServers {
		require.NotNil(t, server.Toolset, server.ID)
		hosts[server.ID] = server.Toolset.HostingMcpServerID
	}
	want := map[uuid.UUID]*string{
		canonical:  conv.PtrEmpty(canonical.String()),
		beside:     conv.PtrEmpty(canonical.String()),
		member:     conv.PtrEmpty(solo.String()),
		solo:       conv.PtrEmpty(solo.String()),
		disabled:   conv.PtrEmpty(disabled.String()),
		enabled:    conv.PtrEmpty(disabled.String()),
		memberOnly: nil,
		retired:    conv.PtrEmpty(live.String()),
		live:       conv.PtrEmpty(live.String()),
	}
	for id, host := range want {
		got, listed := hosts[id.String()]
		require.True(t, listed, id)
		require.Equal(t, host, got, id)
	}

	got, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{ID: conv.PtrEmpty(solo.String())})
	require.NoError(t, err)
	require.Equal(t, conv.PtrEmpty(solo.String()), got.Toolset.HostingMcpServerID)
}

func TestGetMcpServer_ToolsetSummaryWithoutHostWhenEndpointsFail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	server, _ := createToolsetBackedServerFixture(t, ctx, ti, "hosting endpoint failure")

	_, err := ti.conn.Exec(ctx, "ALTER TABLE mcp_endpoints RENAME TO unavailable_mcp_endpoints") //nolint:glint // notestingrawsql: isolated test-database fault injection; never expose destructive DDL through production SQLc methods.
	require.NoError(t, err)
	got, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{ID: &server.ID})
	require.NoError(t, err)
	require.NotNil(t, got.Toolset)
	require.Nil(t, got.Toolset.HostingMcpServerID, "unknown, so clients fall back to the canonical rule")
}
