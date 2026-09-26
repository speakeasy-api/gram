package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

func TestMetaMCPNetworkRequestAttribution(t *testing.T) {
	t.Parallel()
	storedGatewayID := uuid.New()
	require.True(t, shouldRecordMetaMCPNetworkRequest(uuid.Nil))
	attributes := mcpNetworkRequestAttributes(context.Background(), uuid.Nil, storedGatewayID)
	require.Equal(t, MCPNetworkRequestEventURN, attributes[attr.EventURNKey])
	require.Equal(t, storedGatewayID.String(), attributes[attr.MetaMcpServerIDKey])
	_, hasServerID := attributes[attr.McpServerIDKey]
	require.False(t, hasServerID)

	require.False(t, shouldRecordMetaMCPNetworkRequest(uuid.New()))
}
