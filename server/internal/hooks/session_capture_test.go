package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
)

type alwaysEnabledFeatures struct{}

func (alwaysEnabledFeatures) IsFeatureEnabled(_ context.Context, _ string, _ productfeatures.Feature) (bool, error) {
	return true, nil
}

func enableSessionCapture(ti *testInstance) {
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.eventWriter.productFeatures = alwaysEnabledFeatures{}
}

func writeClaudeHookForTest(t *testing.T, ctx context.Context, ti *testInstance, payload *gen.ClaudePayload, metadata *SessionMetadata) {
	t.Helper()
	ev, err := ti.service.normalizeClaudeHookEvent(ctx, payload, time.Now())
	require.NoError(t, err)
	require.NotNil(t, ev)
	require.NoError(t, ti.service.eventWriter.Write(ctx, ev, metadata, WriteOptions{BlockReason: "", SkipChat: false}))
}

// TestClaudeHookSource_ConsistentAcrossAllWrites asserts that every
// chat_messages row produced by a single Claude Code session carries the same
// Source value, regardless of which hook handler wrote it. This guards
// against drift between the conversation-event path
// (UserPromptSubmit/Stop) and the tool-call paths (Pre/PostToolUse).
func TestClaudeHookSource_ConsistentAcrossAllWrites(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	// The feature flag check short-circuits writes when productFeatures is nil
	// (the default in newTestHooksService). Swap in a stub that enables it.
	enableSessionCapture(ti)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	const wantSource = "test-agent-source"
	const wantUserID = "session-capture-user"
	const wantUserEmail = "tester@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, wantUserID, wantUserEmail)

	metadata := &SessionMetadata{
		SessionID:   sessionID,
		ServiceName: wantSource,
		UserEmail:   wantUserEmail,
		UserID:      wantUserID,
		ClaudeOrgID: "",
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}

	toolUseID := "toolu_consistency_" + uuid.NewString()
	toolName := "Edit"
	model := "claude-opus"
	prompt := "hello"
	lastAssistantMessage := "ok"
	toolResponse := map[string]any{"ok": true}
	errorData := map[string]any{"message": "boom"}

	// Each of these is a distinct write path that previously either used
	// metadata.ServiceName or a hardcoded string. The fix unified them — this
	// test asserts the unification stays unified.
	writeClaudeHookForTest(t, ctx, ti, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
	}, metadata)

	writeClaudeHookForTest(t, ctx, ti, &gen.ClaudePayload{
		HookEventName:        "Stop",
		SessionID:            &sessionID,
		LastAssistantMessage: &lastAssistantMessage,
		Model:                &model,
	}, metadata)

	writeClaudeHookForTest(t, ctx, ti, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"file_path": "/tmp/x.go"},
		Model:         &model,
	}, metadata)

	writeClaudeHookForTest(t, ctx, ti, &gen.ClaudePayload{
		HookEventName: "PostToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolResponse:  toolResponse,
	}, metadata)

	writeClaudeHookForTest(t, ctx, ti, &gen.ClaudePayload{
		HookEventName: "PostToolUseFailure",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		Error:         errorData,
	}, metadata)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 5, "expected one chat_messages row per hook write")

	for _, m := range msgs {
		assert.True(t, m.Source.Valid, "Source should be set (role=%s)", m.Role)
		assert.Equal(t, wantSource, m.Source.String,
			"Source should match metadata.ServiceName for all hook writes (role=%s)", m.Role)
		assert.True(t, m.UserID.Valid, "UserID should be set (role=%s)", m.Role)
		assert.Equal(t, wantUserID, m.UserID.String,
			"UserID should match metadata.UserID for all hook writes (role=%s)", m.Role)
	}
}

func TestClaudeUserPromptSubmitDoesNotPersistPromptIDAsMessageID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	enableSessionCapture(ti)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	const wantPromptID = "prompt_01JZTEST"
	const wantUserID = "session-capture-user"
	const wantUserEmail = "tester@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, wantUserID, wantUserEmail)

	metadata := &SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		UserEmail:   wantUserEmail,
		UserID:      wantUserID,
		ClaudeOrgID: "",
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}
	prompt := "hello"

	writeClaudeHookForTest(t, ctx, ti, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
		AdditionalData: map[string]any{
			"promptId": wantPromptID,
		},
	}, metadata)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, "user", msgs[0].Role)
	require.False(t, msgs[0].MessageID.Valid)
	require.False(t, msgs[0].ExternalMessageID.Valid, "Claude prompt IDs must not be stored as external_message_id")
}

func TestClaudeStopBackfillsLatestUserPromptID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	enableSessionCapture(ti)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	const stalePromptID = "prompt-stale"
	const wantPromptID = "prompt-current"
	const wantUserID = "session-capture-user"
	const wantUserEmail = "tester@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, wantUserID, wantUserEmail)

	metadata := &SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		UserEmail:   wantUserEmail,
		UserID:      wantUserID,
		ClaudeOrgID: "",
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}
	prompt := "hello"
	lastAssistantMessage := "ok"

	writeClaudeHookForTest(t, ctx, ti, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
		AdditionalData: map[string]any{
			"promptId": stalePromptID,
		},
	}, metadata)

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, "user", msgs[0].Role)
	require.False(t, msgs[0].MessageID.Valid, "UserPromptSubmit promptId must not be persisted directly")

	writeClaudeHookForTest(t, ctx, ti, &gen.ClaudePayload{
		HookEventName:        "Stop",
		SessionID:            &sessionID,
		LastAssistantMessage: &lastAssistantMessage,
		AdditionalData: map[string]any{
			"LastUserPromptID": wantPromptID,
		},
	}, metadata)

	msgs, err = chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, "user", msgs[0].Role)
	require.True(t, msgs[0].MessageID.Valid)
	require.Equal(t, wantPromptID, msgs[0].MessageID.String)
	require.Equal(t, "assistant", msgs[1].Role)
	require.False(t, msgs[1].MessageID.Valid)
}
