package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

func TestPlatformUsageAttribution(t *testing.T) {
	t.Parallel()
	// Other usage fields must survive attribution, including metered bytes and
	// the original request URL. Neither built-ins nor platform toolsets have a
	// persisted MCP endpoint row to attribute.
	base := billing.ToolCallUsageEvent{
		OrganizationID:     "org_example",
		ProjectID:          "project_example",
		ToolName:           "platform_slack_send_message",
		RequestBytes:       42,
		OutputBytes:        84,
		ResponseStatusCode: 200,
		MCPURL:             conv.PtrEmpty("https://mcp.example/mcp/builtin/organization/project/slack"),
	}
	for _, tc := range []struct {
		name      string
		config    *platformCallConfig
		serverID  string
		gatewayID string
	}{
		{name: "legacy platform call", config: nil},
		{name: "direct built-in", config: &platformCallConfig{serverID: "builtin_slack"}, serverID: "builtin_slack"},
		{name: "gateway built-in", config: &platformCallConfig{serverID: "builtin_slack", metaServerID: "gateway"}, serverID: "builtin_slack", gatewayID: "gateway"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want := base
			want.MCPServerID = conv.PtrEmpty(tc.serverID)
			want.MetaMCPServerID = conv.PtrEmpty(tc.gatewayID)
			got := platformUsageAttribution(tc.config, base)
			require.Equal(t, want, got)
			require.Nil(t, got.MCPEndpointID)
			require.Nil(t, base.MCPServerID, "input event must remain unchanged")
			require.Nil(t, base.MetaMCPServerID, "input event must remain unchanged")
		})
	}
}
