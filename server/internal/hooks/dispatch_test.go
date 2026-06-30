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
