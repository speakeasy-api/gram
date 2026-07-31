package mcpservers_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestGetMcpServer_ToolsetBackedCarriesBackendKindAndSummary(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	toolURNs := []urn.Tool{
		urn.NewTool(urn.ToolKindHTTP, "test-api", "list_things"),
		urn.NewTool(urn.ToolKindHTTP, "test-api", "get_thing"),
	}
	_, err := toolsetsrepo.New(ti.conn).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
		ToolsetID:     toolset.ID,
		Version:       1,
		ToolUrns:      toolURNs,
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)

	toolsetID := toolset.ID.String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "toolset backed server",
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		TunneledMcpServerID:   nil,
		ToolsetID:             &toolsetID,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	fetched, err := ti.service.GetMcpServer(ctx, &gen.GetMcpServerPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ID:               &created.ID,
		Slug:             nil,
	})
	require.NoError(t, err)

	require.NotNil(t, fetched.BackendKind)
	require.Equal(t, types.McpServerBackendKind("toolset"), *fetched.BackendKind)
	require.NotNil(t, fetched.ToolsetSummary)
	require.Equal(t, toolsetID, fetched.ToolsetSummary.ID)
	require.Equal(t, toolset.Slug, fetched.ToolsetSummary.Slug)
	require.Equal(t, toolset.Name, fetched.ToolsetSummary.Name)
	require.Equal(t, 2, fetched.ToolsetSummary.ToolCount)
	require.ElementsMatch(t, []string{toolURNs[0].String(), toolURNs[1].String()}, fetched.ToolsetSummary.ToolUrns)
}

func TestListMcpServers_BackendKindsAndToolsetSummary(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	// The toolset has no version rows: the summary must still be present with
	// a zero tool count.
	toolset := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	toolsetID := toolset.ID.String()

	remoteServer, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "remote backed server",
		EnvironmentID:         nil,
		RemoteMcpServerID:     &remoteID,
		TunneledMcpServerID:   nil,
		ToolsetID:             nil,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	toolsetServer, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "toolset backed server",
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		TunneledMcpServerID:   nil,
		ToolsetID:             &toolsetID,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	result, err := ti.service.ListMcpServers(ctx, &gen.ListMcpServersPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		RemoteMcpServerID:   nil,
		TunneledMcpServerID: nil,
		ToolsetID:           nil,
	})
	require.NoError(t, err)
	require.Len(t, result.McpServers, 2)

	byID := make(map[string]*types.McpServer, len(result.McpServers))
	for _, server := range result.McpServers {
		byID[server.ID] = server
	}

	remoteView := byID[remoteServer.ID]
	require.NotNil(t, remoteView)
	require.NotNil(t, remoteView.BackendKind)
	require.Equal(t, types.McpServerBackendKind("remote"), *remoteView.BackendKind)
	require.Nil(t, remoteView.ToolsetSummary)

	toolsetView := byID[toolsetServer.ID]
	require.NotNil(t, toolsetView)
	require.NotNil(t, toolsetView.BackendKind)
	require.Equal(t, types.McpServerBackendKind("toolset"), *toolsetView.BackendKind)
	require.NotNil(t, toolsetView.ToolsetSummary)
	require.Equal(t, toolsetID, toolsetView.ToolsetSummary.ID)
	require.Equal(t, 0, toolsetView.ToolsetSummary.ToolCount)
	require.Empty(t, toolsetView.ToolsetSummary.ToolUrns)
}
