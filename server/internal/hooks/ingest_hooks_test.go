package hooks

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestIngestCodex_AttributesToAuthenticatedTokenOwner(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	require.NotNil(t, authCtx.ProjectID)

	reportedUserEmail := "reported-codex-user@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "reported-codex-user", reportedUserEmail)

	sessionID := "codex-ingest-auth-owner"
	result, err := ti.service.Ingest(ctx, &gen.IngestPayload{
		HookSource:        "codex",
		EventType:         "session_start",
		HookEventName:     new("SessionStart"),
		SessionID:         &sessionID,
		UserEmail:         &reportedUserEmail,
		ReportedUserEmail: &reportedUserEmail,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	metadata := ti.service.codexSessionMetadata(ctx, &gen.CodexPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
	}, authCtx.ActiveOrganizationID, authCtx.ProjectID.String())
	require.Equal(t, *authCtx.Email, metadata.UserEmail)
	require.Equal(t, authCtx.UserID, metadata.UserID)
}

func TestIngestClaude_AttributesToAuthenticatedTokenOwner(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)
	require.NotNil(t, authCtx.ProjectID)

	reportedUserEmail := "reported-claude-user@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "reported-claude-user", reportedUserEmail)

	sessionID := "claude-ingest-auth-owner"
	result, err := ti.service.Ingest(ctx, &gen.IngestPayload{
		HookSource:        "claude",
		EventType:         "session_start",
		HookEventName:     new("SessionStart"),
		SessionID:         &sessionID,
		UserEmail:         &reportedUserEmail,
		ReportedUserEmail: &reportedUserEmail,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	metadata, err := ti.service.getSessionMetadata(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, *authCtx.Email, metadata.UserEmail)
	require.Equal(t, authCtx.UserID, metadata.UserID)
	require.Equal(t, authCtx.ActiveOrganizationID, metadata.GramOrgID)
	require.Equal(t, authCtx.ProjectID.String(), metadata.ProjectID)
}

func TestIngest_AcceptsCustomHookSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestHooksService(t)
	sessionID := "custom-ingest-source"

	result, err := ti.service.Ingest(ctx, &gen.IngestPayload{
		HookSource: "openclaw",
		EventType:  "session_start",
		SessionID:  &sessionID,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}
