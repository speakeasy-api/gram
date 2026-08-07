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
