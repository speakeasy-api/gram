package remotemcp_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/toolcallobserver"
)

type selectedUseCapture struct {
	mu           sync.Mutex
	observations []toolcallobserver.SuccessObservation
	done         chan struct{}
}

func (c *selectedUseCapture) RecordSuccessfulToolCall(_ context.Context, observation toolcallobserver.SuccessObservation) {
	c.mu.Lock()
	c.observations = append(c.observations, observation)
	c.mu.Unlock()
	select {
	case c.done <- struct{}{}:
	default:
	}
}

func TestPlatformMCPSelectedUseInterceptorRecordsOnlyToolSuccess(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	serverID := uuid.New()
	capture := &selectedUseCapture{done: make(chan struct{}, 1)}
	interceptor := remotemcp.NewPlatformMCPSelectedUseInterceptor(capture, proxy.ServerIdentity{McpServerID: serverID.String()})
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-test",
		UserID:               "user-test",
		ProjectID:            &projectID,
	})

	call := newToolsCallResponseForInterceptor(t, "")
	require.NoError(t, interceptor.InterceptToolsCallResponse(ctx, call))

	select {
	case <-capture.done:
	case <-t.Context().Done():
		t.Fatal("selected-use observer did not record a successful tool call")
	}
	capture.mu.Lock()
	require.Len(t, capture.observations, 1)
	observation := capture.observations[0]
	capture.mu.Unlock()
	require.Equal(t, "org-test", observation.OrganizationID)
	require.Equal(t, "user-test", observation.UserID)
	require.Equal(t, projectID, observation.ProjectID)
	require.Equal(t, serverID, observation.MCPServerID)
	require.Equal(t, "search_tickets", observation.ToolName)
	require.False(t, observation.SucceededAt.IsZero())

	call.Result = &mcp.CallToolResult{IsError: true}
	require.NoError(t, interceptor.InterceptToolsCallResponse(ctx, call))
	call.Result = nil
	require.NoError(t, interceptor.InterceptToolsCallResponse(ctx, call))

	capture.mu.Lock()
	defer capture.mu.Unlock()
	require.Len(t, capture.observations, 1, "tool-level and JSON-RPC failures must not count as selected use")
}
