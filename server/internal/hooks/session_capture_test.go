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

	metadata := &SessionMetadata{
		SessionID:   sessionID,
		ServiceName: wantSource,
		UserEmail:   "tester@example.com",
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
	require.NoError(t, ti.service.persistConversationEvent(ctx, &gen.ClaudeHookPayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
	}, metadata))

	require.NoError(t, ti.service.persistConversationEvent(ctx, &gen.ClaudeHookPayload{
		HookEventName:        "Stop",
		SessionID:            &sessionID,
		LastAssistantMessage: &lastAssistantMessage,
		Model:                &model,
	}, metadata))

	require.NoError(t, ti.service.writeToolCallRequestToPG(ctx, &gen.ClaudeHookPayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"file_path": "/tmp/x.go"},
		Model:         &model,
	}, metadata))

	require.NoError(t, ti.service.writeToolCallResultToPG(ctx, &gen.ClaudeHookPayload{
		HookEventName: "PostToolUse",
		SessionID:     &sessionID,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolResponse:  toolResponse,
	}, metadata))

	require.NoError(t, ti.service.writeToolCallResultToPG(ctx, &gen.ClaudeHookPayload{
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
	}
}
