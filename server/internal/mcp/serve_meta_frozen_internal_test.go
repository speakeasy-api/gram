package mcp

import (
	"context"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFrozenHostedPlanRejectsConcurrentDeployment(t *testing.T) {
	t.Parallel()
	member := metaMember{serverID: uuid.New(), slug: "hosted", backend: metaMemberBackendHosted}
	tool := &types.Tool{HTTPToolDefinition: &types.HTTPToolDefinition{ID: uuid.NewString(), ToolUrn: "tools:http:fixture:read", Name: "read", Schema: `{"type":"object"}`}}
	entry := toolToListEntry(tool)
	require.NotEmpty(t, entry.routingIdentity)
	approved, err := frozenMemberTool(member, "route", entry)
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), frozenHostedExecutionKey{}, frozenHostedExecution{approved: &toolfilter.FrozenToolset{Tools: []toolfilter.FrozenTool{approved}}, member: member, routing: "route"})
	plan := &gateway.ToolCallPlan{Descriptor: &gateway.ToolDescriptor{ID: tool.HTTPToolDefinition.ID}}
	require.NoError(t, validateFrozenHostedTool(ctx, tool, plan))
	plan.Descriptor.ID = uuid.NewString()
	require.ErrorContains(t, validateFrozenHostedTool(ctx, tool, plan), "execution changed during validation")
}
