package mcpservers_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestListMcpServersForOrg_AllBackends(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	tunneledID := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	toolsetID := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID).ID.String()

	backends := []struct {
		name    string
		payload gen.CreateMcpServerPayload
	}{
		{name: "remote server", payload: gen.CreateMcpServerPayload{
			SessionToken:          nil,
			ApikeyToken:           nil,
			ProjectSlugInput:      nil,
			Name:                  "remote server",
			EnvironmentID:         nil,
			RemoteMcpServerID:     &remoteID,
			TunneledMcpServerID:   nil,
			ToolsetID:             nil,
			ToolVariationsGroupID: nil,
			Visibility:            types.McpServerVisibility("disabled"),
		}},
		{name: "tunneled server", payload: gen.CreateMcpServerPayload{
			SessionToken:          nil,
			ApikeyToken:           nil,
			ProjectSlugInput:      nil,
			Name:                  "tunneled server",
			EnvironmentID:         nil,
			RemoteMcpServerID:     nil,
			TunneledMcpServerID:   &tunneledID,
			ToolsetID:             nil,
			ToolVariationsGroupID: nil,
			Visibility:            types.McpServerVisibility("disabled"),
		}},
		{name: "toolset server", payload: gen.CreateMcpServerPayload{
			SessionToken:          nil,
			ApikeyToken:           nil,
			ProjectSlugInput:      nil,
			Name:                  "toolset server",
			EnvironmentID:         nil,
			RemoteMcpServerID:     nil,
			TunneledMcpServerID:   nil,
			ToolsetID:             &toolsetID,
			ToolVariationsGroupID: nil,
			Visibility:            types.McpServerVisibility("disabled"),
		}},
	}
	for _, b := range backends {
		_, err := ti.service.CreateMcpServer(ctx, &b.payload)
		require.NoError(t, err)
	}

	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	result, err := ti.service.ListMcpServersForOrg(ctx, &gen.ListMcpServersForOrgPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.McpServers, 3)

	byName := make(map[string]*types.McpServer, len(result.McpServers))
	for _, s := range result.McpServers {
		require.NotNil(t, s.Name)
		byName[*s.Name] = s
	}
	require.NotNil(t, byName["remote server"].RemoteMcpServerID)
	require.Equal(t, remoteID, *byName["remote server"].RemoteMcpServerID)
	require.NotNil(t, byName["tunneled server"].TunneledMcpServerID)
	require.Equal(t, tunneledID, *byName["tunneled server"].TunneledMcpServerID)
	require.NotNil(t, byName["toolset server"].ToolsetID)
	require.Equal(t, toolsetID, *byName["toolset server"].ToolsetID)

	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestListMcpServersForOrg_Empty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	result, err := ti.service.ListMcpServersForOrg(ctx, &gen.ListMcpServersForOrgPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Empty(t, result.McpServers)
}

func TestListMcpServersForOrg_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestService(t)

	// Context with no auth context at all.
	_, err := ti.service.ListMcpServersForOrg(t.Context(), &gen.ListMcpServersForOrgPayload{
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestListMcpServersForOrg_WithoutProjectID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "no project scope server",
		EnvironmentID:         nil,
		RemoteMcpServerID:     &remoteID,
		TunneledMcpServerID:   nil,
		ToolsetID:             nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	// Remove project from auth context — simulates the RBAC page which has no
	// project slug in the URL.
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	result, err := ti.service.ListMcpServersForOrg(ctx, &gen.ListMcpServersForOrgPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.McpServers, 1)
	require.Equal(t, created.ID, result.McpServers[0].ID)
}

func TestListMcpServersForOrg_CrossProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	remoteA := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	serverA, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "project A server",
		EnvironmentID:         nil,
		RemoteMcpServerID:     &remoteA,
		TunneledMcpServerID:   nil,
		ToolsetID:             nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	// Create a second project in the same organization.
	projectSlug2 := fmt.Sprintf("test-%s", uuid.New().String()[:8])
	p2, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           projectSlug2,
		Slug:           projectSlug2,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	authCtx.ProjectID = &p2.ID
	authCtx.ProjectSlug = &p2.Slug
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	remoteB := seedRemoteMcpServer(t, ctx, ti.conn, p2.ID).String()
	serverB, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "project B server",
		EnvironmentID:         nil,
		RemoteMcpServerID:     &remoteB,
		TunneledMcpServerID:   nil,
		ToolsetID:             nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	// Clear project scope to simulate the org-wide query.
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	result, err := ti.service.ListMcpServersForOrg(ctx, &gen.ListMcpServersForOrgPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.McpServers, 2)

	projectIDs := make(map[string]bool)
	serverIDs := make(map[string]bool)
	for _, s := range result.McpServers {
		serverIDs[s.ID] = true
		projectIDs[s.ProjectID] = true
	}
	require.True(t, serverIDs[serverA.ID], "server from project A should be present")
	require.True(t, serverIDs[serverB.ID], "server from project B should be present")
	require.Len(t, projectIDs, 2, "servers should come from two different projects")
}

func TestListMcpServersForOrg_ExcludesDeletedServers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	keptRemote := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	kept, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "kept server",
		EnvironmentID:         nil,
		RemoteMcpServerID:     &keptRemote,
		TunneledMcpServerID:   nil,
		ToolsetID:             nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	deletedRemote := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	deleted, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "deleted server",
		EnvironmentID:         nil,
		RemoteMcpServerID:     &deletedRemote,
		TunneledMcpServerID:   nil,
		ToolsetID:             nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	err = ti.service.DeleteMcpServer(ctx, &gen.DeleteMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               deleted.ID,
	})
	require.NoError(t, err)

	result, err := ti.service.ListMcpServersForOrg(ctx, &gen.ListMcpServersForOrgPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.McpServers, 1, "deleted server should not appear")
	require.Equal(t, kept.ID, result.McpServers[0].ID)
}

func TestListMcpServersForOrg_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// A principal with no grants at all lacks org:read.
	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.ListMcpServersForOrg(ctx, &gen.ListMcpServersForOrgPayload{
		SessionToken: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
