package agent_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/hiddenmcpservers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
)

func TestHideMcpServer_SharesTheConsentPagePreference(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	plugin := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering")
	assignPlugin(t, ctx, ti.conn, plugin, ti.orgID, "*")
	linear, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, plugin, "Linear", 1)
	datadog, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, plugin, "Datadog", 2)
	ti.service.WithConnectionStatuses(&stubConnectionStatuses{
		byMCP: map[uuid.UUID]platformmcp.GetMyMCPConnectionStatusOutput{
			linear:  {State: platformmcp.MCPConnectionStateNotConnected, ConnectionURL: testServerURL + "/mcp/linear"},
			datadog: {State: platformmcp.MCPConnectionStateNotConnected, ConnectionURL: testServerURL + "/mcp/datadog"},
		},
	})
	email := new(mockidp.MockUserEmail)

	// Hidden on the consent page: the device agent sees it.
	require.NoError(t, hiddenmcpservers.Hide(ctx, ti.conn, ti.orgID, mockidp.MockUserID, datadog))
	res, err := ti.service.GetMyConnections(ctx, &gen.GetMyConnectionsPayload{Email: email})
	require.NoError(t, err)
	require.Nil(t, connectionByID(t, res, linear).Hidden)
	require.True(t, *connectionByID(t, res, datadog).Hidden)

	// Hidden from the device agent: the consent page and gateways see it.
	require.NoError(t, ti.service.HideMcpServer(ctx, &gen.HideMcpServerPayload{McpServerID: linear.String(), Email: email}))
	require.NoError(t, ti.service.HideMcpServer(ctx, &gen.HideMcpServerPayload{McpServerID: linear.String(), Email: email}), "hiding twice changes nothing")
	hidden, err := hiddenmcpservers.HiddenServerIDs(ctx, ti.conn, ti.orgID, mockidp.MockUserID)
	require.NoError(t, err)
	require.Equal(t, map[uuid.UUID]bool{linear: true, datadog: true}, hidden)

	require.NoError(t, ti.service.UnhideMcpServer(ctx, &gen.UnhideMcpServerPayload{McpServerID: datadog.String(), Email: email}))
	res, err = ti.service.GetMyConnections(ctx, &gen.GetMyConnectionsPayload{Email: email})
	require.NoError(t, err)
	require.True(t, *connectionByID(t, res, linear).Hidden)
	require.Nil(t, connectionByID(t, res, datadog).Hidden)
}

func TestHideMcpServer_OnlyDeliveredServers(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	delivered := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "engineering")
	assignPlugin(t, ctx, ti.conn, delivered, ti.orgID, "*")
	gateway := seedPluginGateway(t, ctx, ti.conn, ti.orgID, ti.projectID, delivered, "Gateway", 1)
	other := seedProject(t, ctx, ti.conn, ti.orgID, "unpublished")
	undelivered := seedPlugin(t, ctx, ti.conn, ti.orgID, other, "hidden")
	assignPlugin(t, ctx, ti.conn, undelivered, ti.orgID, "*")
	unseen, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, other, undelivered, "Unseen", 1)
	unreadable, _ := seedPluginMCPServer(t, ctx, ti.conn, ti.orgID, ti.projectID, delivered, "Unreadable", 2)
	ti.service.WithConnectionStatuses(&stubConnectionStatuses{
		byMCP:   map[uuid.UUID]platformmcp.GetMyMCPConnectionStatusOutput{},
		missing: map[uuid.UUID]bool{unreadable: true},
		users:   nil,
	})
	email := new(mockidp.MockUserEmail)

	for name, id := range map[string]string{
		"undelivered server": unseen.String(),
		"gateway":            gateway.String(),
		"unknown server":     uuid.NewString(),
		"unreadable server":  unreadable.String(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := ti.service.HideMcpServer(ctx, &gen.HideMcpServerPayload{McpServerID: id, Email: email})
			requireCode(t, err, oops.CodeNotFound)
		})
	}

	err := ti.service.HideMcpServer(ctx, &gen.HideMcpServerPayload{McpServerID: "not-a-uuid", Email: email})
	requireCode(t, err, oops.CodeBadRequest)

	hidden, err := hiddenmcpservers.HiddenServerIDs(ctx, ti.conn, ti.orgID, mockidp.MockUserID)
	require.NoError(t, err)
	require.Empty(t, hidden)
}

func TestHideMcpServer_OrgKeyRequiresVouchedEmail(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	err := ti.service.HideMcpServer(ctx, &gen.HideMcpServerPayload{McpServerID: uuid.NewString(), Email: nil})
	requireCode(t, err, oops.CodeBadRequest)
	err = ti.service.UnhideMcpServer(ctx, &gen.UnhideMcpServerPayload{McpServerID: uuid.NewString(), Email: nil})
	requireCode(t, err, oops.CodeBadRequest)
}
