package hooks

import (
	"context"
	"testing"

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

func (alwaysEnabledFeatures) IsUserSessionCaptureExcluded(_ context.Context, _ string, _ string) (bool, error) {
	return false, nil
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
	ti.service.productFeatures = alwaysEnabledFeatures{}

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
	require.NoError(t, ti.service.persistConversationEvent(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
	}, metadata))

	require.NoError(t, ti.service.persistConversationEvent(ctx, &gen.ClaudePayload{
		HookEventName:        "Stop",
		SessionID:            &sessionID,
		LastAssistantMessage: &lastAssistantMessage,
		Model:                &model,
	}, metadata))

	require.NoError(t, ti.service.writeToolCallRequestToPG(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"file_path": "/tmp/x.go"},
		Model:         &model,
	}, metadata))

	require.NoError(t, ti.service.writeToolCallResultToPG(ctx, &gen.ClaudePayload{
		HookEventName: "PostToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolResponse:  toolResponse,
	}, metadata))

	require.NoError(t, ti.service.writeToolCallResultToPG(ctx, &gen.ClaudePayload{
		HookEventName: "PostToolUseFailure",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		Error:         errorData,
	}, metadata))

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

// excludingFeatures grants session_capture but reports the metadata.UserID as
// excluded — the exact configuration produced when an admin opts a single
// member out of session capture while leaving the org-wide flag on.
type excludingFeatures struct {
	excludedUserID string
}

func (excludingFeatures) IsFeatureEnabled(_ context.Context, _ string, _ productfeatures.Feature) (bool, error) {
	return true, nil
}

func (e excludingFeatures) IsUserSessionCaptureExcluded(_ context.Context, _ string, userID string) (bool, error) {
	return userID == e.excludedUserID, nil
}

// TestSessionCapture_ExcludedUser_NoMessagesWritten asserts that when a user is
// on the per-org exclusion list, the hook ingestion path produces no
// chat_messages rows even with session_capture turned on.
func TestSessionCapture_ExcludedUser_NoMessagesWritten(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	const userID = "excluded-user"
	const userEmail = "excluded@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)

	ti.service.productFeatures = excludingFeatures{excludedUserID: userID}

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	prompt := "this should not be captured"

	metadata := &SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "test-agent",
		UserEmail:   userEmail,
		UserID:      userID,
		ClaudeOrgID: "",
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}

	require.NoError(t, ti.service.persistConversationEvent(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
	}, metadata))

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	assert.Empty(t, msgs, "no chat_messages should be written for excluded users")
}
