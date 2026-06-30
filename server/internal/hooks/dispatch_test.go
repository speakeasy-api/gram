package hooks

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestDispatch_Cursor_ResolvesDefaultProjectAndAllows(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgScoped := *authCtx
	orgScoped.ProjectID = nil
	orgScoped.ProjectSlug = nil
	ctx = contextvalues.SetAuthContext(ctx, &orgScoped)

	res, err := ti.service.Dispatch(ctx, &hooks.DispatchPayload{
		Tool:      "cursor",
		UserEmail: "dev@example.com",
		Payload: map[string]any{
			"hook_event_name": "preToolUse",
			"tool_name":       "Edit",
			"tool_use_id":     "toolu_dispatch_cursor_1",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, res.Permission)
	assert.Equal(t, "allow", *res.Permission)
}

func TestDispatch_Codex_ResolvesDefaultProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgScoped := *authCtx
	orgScoped.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, &orgScoped)

	res, err := ti.service.Dispatch(ctx, &hooks.DispatchPayload{
		Tool:      "codex",
		UserEmail: "dev@example.com",
		Payload: map[string]any{
			"hook_event_name": "PreToolUse",
			"tool_name":       "shell",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, res)
}

func TestDispatch_UnknownProjectSlugErrors(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgScoped := *authCtx
	orgScoped.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, &orgScoped)

	bogus := "does-not-exist"
	_, err := ti.service.Dispatch(ctx, &hooks.DispatchPayload{
		Tool:        "cursor",
		UserEmail:   "dev@example.com",
		ProjectSlug: &bogus,
		Payload:     map[string]any{"hook_event_name": "preToolUse"},
	})
	require.Error(t, err, "an override slug not in the org must error, not silently fall back")
}

// The agent vouches for user_email within its org (DNO-376). For Claude, a
// stale cached session email (e.g. after an AI-tool-account switch — the exact
// bypass we guard against) must not override the vouched email.
func TestDispatch_VouchedEmailBeatsCachedSession(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	org := authCtx.ActiveOrganizationID

	got := ti.service.mergeClaudeAuthContextMetadata(ctx,
		SessionMetadata{UserEmail: "vouched@corp.example", GramOrgID: org},
		SessionMetadata{UserEmail: "switched@personal.example"})
	require.Equal(t, "vouched@corp.example", got.UserEmail,
		"the auth/vouched email must win over a cached session email")

	// An absent auth email still falls back to the cached session email.
	gap := ti.service.mergeClaudeAuthContextMetadata(ctx,
		SessionMetadata{UserEmail: "", GramOrgID: org},
		SessionMetadata{UserEmail: "cached@personal.example"})
	require.Equal(t, "cached@personal.example", gap.UserEmail,
		"cached email should still fill a gap when no email is vouched")
}

func TestDispatch_UnknownToolErrors(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	_, err := ti.service.Dispatch(ctx, &hooks.DispatchPayload{
		Tool:      "intellij",
		UserEmail: "dev@example.com",
		Payload:   map[string]any{"hook_event_name": "x"},
	})
	require.Error(t, err)
}
