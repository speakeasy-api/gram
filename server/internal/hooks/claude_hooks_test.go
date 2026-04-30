package hooks

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

// When the request authenticated via Gram-Key + Gram-Project, handlePreToolUse
// must build SessionMetadata from the auth context instead of short-circuiting
// to "allow" because Redis hasn't been seeded by OTEL Logs yet. Otherwise the
// shadow-MCP guard never fires on the first PreToolUse of a plugin-driven
// session — exactly when the guard is most needed (no toolset_id present yet
// in the conversation's tool history).
func TestClaude_PreToolUse_UsesAuthContextWhenNoCachedMetadata(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID, "test setup should populate ProjectID")

	sessionID := uuid.NewString()
	toolName := "mcp__gram__do_thing"
	toolUseID := "toolu_pretooluse_authctx"

	// Tool input is missing the required x-gram-toolset-id property, so
	// validateGramToolsetCall must deny. Reaching that check at all proves
	// the auth-context branch ran — before the fix, the empty Redis cache
	// would have returned allow without consulting the policy.
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"foo": "bar"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok, "HookSpecificOutput should be a *HookSpecificOutput")
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "deny", *output.PermissionDecision,
		"missing x-gram-toolset-id should be denied once auth-context metadata is in play")
}

// Sanity check the OTEL fallback path: with no auth context and no Redis
// cached metadata, handlePreToolUse should still gracefully allow the call
// rather than erroring (the buffered hook will be re-persisted later).
func TestClaude_PreToolUse_AllowsWhenNoAuthAndNoCachedMetadata(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	bareCtx := t.Context()
	sessionID := uuid.NewString()
	toolName := "mcp__gram__do_thing"
	toolUseID := "toolu_pretooluse_noauth"

	result, err := ti.service.Claude(bareCtx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"foo": "bar"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	assert.Equal(t, "allow", *output.PermissionDecision,
		"OTEL path with no metadata should default to allow so first call isn't blocked")
}
