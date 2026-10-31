package hooks

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// installCheckPayload is the tool call the agent install script sends through
// a freshly installed OpenClaw plugin.
func installCheckPayload(key, projectSlug string) *gen.IngestPayload {
	toolCallID := "speakeasy-smoke-1"
	toolName := "exec"
	payload := canonicalIngestPayload("openclaw", "tool.requested", InstallCheckSessionID)
	payload.ApikeyToken = &key
	payload.ProjectSlugInput = &projectSlug
	payload.Data = &gen.HookIngestData{ToolCall: &gen.HookToolCallData{
		ID:             &toolCallID,
		Name:           &toolName,
		Input:          map[string]any{"command": "true"},
		Output:         nil,
		Error:          nil,
		IsInterrupt:    nil,
		PermissionType: nil,
	}}
	return payload
}

// An install check from an agent allowed to send hooks passes, and leaves no
// session behind.
func TestIngest_InstallCheckIsAllowedAndNotRecorded(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	agentKey := newHooksAgentKey(t, ctx, ti, projectHooksGrant(authCtx.ActiveOrganizationID, *authCtx.ProjectID))

	result, err := ti.service.Ingest(t.Context(), installCheckPayload(agentKey.key, *authCtx.ProjectSlug))
	require.NoError(t, err)
	require.Equal(t, "allow", result.Decision)
	requireNothingRecorded(t, ctx, ti, agentKey.sessionID(InstallCheckSessionID), *authCtx.ProjectID)
}

// The check still proves the key may send hooks: without org:hooks_ingest it
// is refused like any other event.
func TestIngest_InstallCheckRefusedWithoutTheHooksGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	agentKey := newHooksAgentKey(t, ctx, ti, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))

	_, err := ti.service.Ingest(t.Context(), installCheckPayload(agentKey.key, *authCtx.ProjectSlug))
	requireShareableCode(t, err, oops.CodeForbidden)
}
