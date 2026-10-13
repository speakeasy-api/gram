package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
)

func TestServingAttribution_OmitsUnknownIDs(t *testing.T) {
	t.Parallel()

	payload := &mcpInputs{}
	props := map[string]any{}
	logAttrs := tm.HTTPLogAttributes{}
	payload.recordServingAnalytics(props)
	recordServingLogAttrs(logAttrs, payload.mcpServerID, payload.mcpEndpointID)
	require.Empty(t, props)
	require.Empty(t, logAttrs)

	serverID, endpointID := uuid.New(), uuid.New()
	payload.mcpServerID, payload.mcpEndpointID = &serverID, &endpointID
	payload.recordServingAnalytics(props)
	recordServingLogAttrs(logAttrs, payload.mcpServerID, payload.mcpEndpointID)
	require.Equal(t, map[string]any{"mcp_server_id": serverID.String(), "mcp_endpoint_id": endpointID.String()}, props)
	require.Equal(t, serverID.String(), logAttrs[attr.McpServerIDKey])
	require.Equal(t, endpointID.String(), logAttrs[attr.McpEndpointIDKey])
}

func TestServingAttribution_DoesNotChangeAuthorizationIdentity(t *testing.T) {
	t.Parallel()
	fallback, fronting := uuid.New(), uuid.New()
	payload := &mcpInputs{attributionServerID: &fallback}
	require.Equal(t, &fallback, payload.servingServerID())
	require.Nil(t, payload.mcpServerID)
	payload.mcpServerID = &fronting
	require.Equal(t, &fronting, payload.servingServerID())
}
