package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/gateway"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
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
	tool.HTTPToolDefinition.Schema = `{"type":"object","description":"changed"}`
	require.ErrorContains(t, validateFrozenHostedTool(ctx, tool, plan), "approved frozen toolset")
	plan.Descriptor.ID = uuid.NewString()
	require.ErrorContains(t, validateFrozenHostedTool(ctx, tool, plan), "execution changed during validation")
}

func TestFrozenWireRejectsNullToolDefinitions(t *testing.T) {
	t.Parallel()
	var entry toolListEntry
	require.Error(t, json.Unmarshal([]byte("null"), &entry))
	_, err := json.Marshal(toolListEntry{Name: "read", rawDefinition: json.RawMessage("null")})
	require.ErrorContains(t, err, "must be an object")
	_, err = json.Marshal(metamcp.SchemaTool{Name: "read", RawDefinition: json.RawMessage("null")})
	require.ErrorContains(t, err, "must be an object")
}
