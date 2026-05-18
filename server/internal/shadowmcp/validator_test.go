package shadowmcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func TestValidateToolsetCall_NonMapInput(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	detail, denied := f.client.ValidateToolsetCall(t.Context(), "not a map", "tool", f.orgID)
	assert.True(t, denied)
	assert.Contains(t, detail, shadowmcp.XGramToolsetIDField)
}

func TestValidateToolsetCall_NilInput(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	detail, denied := f.client.ValidateToolsetCall(t.Context(), nil, "tool", f.orgID)
	assert.True(t, denied)
	assert.Contains(t, detail, shadowmcp.XGramToolsetIDField)
}

func TestValidateToolsetCall_MissingToolsetIDKey(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	detail, denied := f.client.ValidateToolsetCall(t.Context(), map[string]any{"foo": "bar"}, "tool", f.orgID)
	assert.True(t, denied)
	assert.Contains(t, detail, shadowmcp.XGramToolsetIDField)
}

func TestValidateToolsetCall_EmptyToolsetID(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	detail, denied := f.client.ValidateToolsetCall(t.Context(), map[string]any{shadowmcp.XGramToolsetIDField: ""}, "tool", f.orgID)
	assert.True(t, denied)
	assert.Contains(t, detail, shadowmcp.XGramToolsetIDField)
}

func TestValidateToolsetCall_NonStringToolsetID(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	detail, denied := f.client.ValidateToolsetCall(t.Context(), map[string]any{shadowmcp.XGramToolsetIDField: 123}, "tool", f.orgID)
	assert.True(t, denied)
	assert.Contains(t, detail, shadowmcp.XGramToolsetIDField)
}

func TestValidateToolsetCall_InvalidUUID(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	detail, denied := f.client.ValidateToolsetCall(t.Context(), map[string]any{shadowmcp.XGramToolsetIDField: "not-a-uuid"}, "tool", f.orgID)
	assert.True(t, denied)
	assert.Contains(t, detail, "not a UUID")
}

func TestValidateToolsetCall_ToolsetNotFound(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	missingID := uuid.New().String()
	detail, denied := f.client.ValidateToolsetCall(t.Context(), map[string]any{shadowmcp.XGramToolsetIDField: missingID}, "tool", f.orgID)
	assert.True(t, denied)
	assert.Contains(t, detail, "not found in this organization")
	assert.Contains(t, detail, missingID)
}

func TestValidateToolsetCall_ToolsetInOtherOrg(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	toolsetID := f.createToolset(t, "ts-"+uuid.NewString()[:8])

	detail, denied := f.client.ValidateToolsetCall(
		t.Context(),
		map[string]any{shadowmcp.XGramToolsetIDField: toolsetID.String()},
		"tool",
		"some-other-org",
	)
	assert.True(t, denied)
	assert.Contains(t, detail, "not found in this organization")
}

func TestValidateToolsetCall_EmptyToolName(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	toolsetID := f.createToolset(t, "ts-"+uuid.NewString()[:8])

	detail, denied := f.client.ValidateToolsetCall(
		t.Context(),
		map[string]any{shadowmcp.XGramToolsetIDField: toolsetID.String()},
		"",
		f.orgID,
	)
	assert.True(t, denied)
	assert.Contains(t, detail, "missing tool name")
}

func TestValidateToolsetCall_ToolNotInToolset(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	toolsetID := f.createToolset(t, "ts-"+uuid.NewString()[:8])

	detail, denied := f.client.ValidateToolsetCall(
		t.Context(),
		map[string]any{shadowmcp.XGramToolsetIDField: toolsetID.String()},
		"unknown-tool",
		f.orgID,
	)
	assert.True(t, denied)
	assert.Contains(t, detail, "unknown-tool")
	assert.Contains(t, detail, "not part of toolset")
}

func TestResolveToolsetCall_ReturnsToolAnnotations(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	destructive := true
	toolsetID := f.createToolsetWithHTTPTool(t, "ts-"+uuid.NewString()[:8], "delete_records", &destructive)
	detail, denied := f.client.ValidateToolsetCall(
		t.Context(),
		map[string]any{shadowmcp.XGramToolsetIDField: toolsetID.String()},
		"delete_records",
		f.orgID,
	)
	require.False(t, denied, detail)

	resolved, ok := f.client.ResolveToolsetCall(
		t.Context(),
		map[string]any{shadowmcp.XGramToolsetIDField: toolsetID.String()},
		"delete_records",
		f.orgID,
	)
	require.True(t, ok)
	require.NotNil(t, resolved)
	require.Equal(t, toolsetID.String(), resolved.ToolsetID)
	require.Equal(t, "delete_records", resolved.ToolName)
	require.NotNil(t, resolved.Tool.Annotations)
	require.NotNil(t, resolved.Tool.Annotations.DestructiveHint)
	require.True(t, *resolved.Tool.Annotations.DestructiveHint)
}

func TestResolveToolsetCall_MissingToolsetIDReturnsNoResult(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	resolved, ok := f.client.ResolveToolsetCall(
		t.Context(),
		map[string]any{"foo": "bar"},
		"delete_records",
		f.orgID,
	)
	require.False(t, ok)
	require.Nil(t, resolved)
}

func TestValidateRemoteMCPServerCall_NonMapInput(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	detail, denied := f.client.ValidateRemoteMCPServerCall(t.Context(), "not a map", "tool", f.projectID.String())
	assert.True(t, denied)
	assert.Contains(t, detail, shadowmcp.XGramToolsetIDField)
}

func TestValidateRemoteMCPServerCall_NilInput(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	detail, denied := f.client.ValidateRemoteMCPServerCall(t.Context(), nil, "tool", f.projectID.String())
	assert.True(t, denied)
	assert.Contains(t, detail, shadowmcp.XGramToolsetIDField)
}

func TestValidateRemoteMCPServerCall_MissingToolsetIDKey(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	detail, denied := f.client.ValidateRemoteMCPServerCall(t.Context(), map[string]any{"foo": "bar"}, "tool", f.projectID.String())
	assert.True(t, denied)
	assert.Contains(t, detail, shadowmcp.XGramToolsetIDField)
}

func TestValidateRemoteMCPServerCall_EmptyToolsetID(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	detail, denied := f.client.ValidateRemoteMCPServerCall(t.Context(), map[string]any{shadowmcp.XGramToolsetIDField: ""}, "tool", f.projectID.String())
	assert.True(t, denied)
	assert.Contains(t, detail, shadowmcp.XGramToolsetIDField)
}

func TestValidateRemoteMCPServerCall_NonStringToolsetID(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	detail, denied := f.client.ValidateRemoteMCPServerCall(t.Context(), map[string]any{shadowmcp.XGramToolsetIDField: 123}, "tool", f.projectID.String())
	assert.True(t, denied)
	assert.Contains(t, detail, shadowmcp.XGramToolsetIDField)
}

func TestValidateRemoteMCPServerCall_InvalidUUID(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	detail, denied := f.client.ValidateRemoteMCPServerCall(t.Context(), map[string]any{shadowmcp.XGramToolsetIDField: "not-a-uuid"}, "tool", f.projectID.String())
	assert.True(t, denied)
	assert.Contains(t, detail, "not a UUID")
}

func TestValidateRemoteMCPServerCall_InvalidProjectID(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	detail, denied := f.client.ValidateRemoteMCPServerCall(
		t.Context(),
		map[string]any{shadowmcp.XGramToolsetIDField: uuid.New().String()},
		"tool",
		"not-a-uuid",
	)
	assert.True(t, denied)
	assert.Contains(t, detail, "invalid project id")
}

func TestValidateRemoteMCPServerCall_ServerNotFound(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	missingID := uuid.New().String()
	detail, denied := f.client.ValidateRemoteMCPServerCall(
		t.Context(),
		map[string]any{shadowmcp.XGramToolsetIDField: missingID},
		"tool",
		f.projectID.String(),
	)
	assert.True(t, denied)
	assert.Contains(t, detail, "not found in this project")
	assert.Contains(t, detail, missingID)
}

func TestValidateRemoteMCPServerCall_ServerInOtherProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	serverID := f.createRemoteMCPServer(t, "srv-"+uuid.NewString()[:8])

	// A real server exists, but the request claims a different project —
	// the lookup is project-scoped, so cross-project echoes are rejected.
	detail, denied := f.client.ValidateRemoteMCPServerCall(
		t.Context(),
		map[string]any{shadowmcp.XGramToolsetIDField: serverID.String()},
		"tool",
		uuid.New().String(),
	)
	assert.True(t, denied)
	assert.Contains(t, detail, "not found in this project")
}

func TestValidateRemoteMCPServerCall_EmptyToolName(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	serverID := f.createRemoteMCPServer(t, "srv-"+uuid.NewString()[:8])

	detail, denied := f.client.ValidateRemoteMCPServerCall(
		t.Context(),
		map[string]any{shadowmcp.XGramToolsetIDField: serverID.String()},
		"",
		f.projectID.String(),
	)
	assert.True(t, denied)
	assert.Contains(t, detail, "missing tool name")
}

func TestValidateRemoteMCPServerCall_Success(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	serverID := f.createRemoteMCPServer(t, "srv-"+uuid.NewString()[:8])

	// A valid, project-scoped server UUID with a non-empty tool name
	// passes validation. Tool-name catalog cross-check is deliberately
	// skipped — remote MCP servers expose their catalog dynamically and
	// Gram does not mirror it.
	detail, denied := f.client.ValidateRemoteMCPServerCall(
		t.Context(),
		map[string]any{shadowmcp.XGramToolsetIDField: serverID.String()},
		"arbitrary-tool-name",
		f.projectID.String(),
	)
	require.False(t, denied, detail)
	require.Empty(t, detail)
}
