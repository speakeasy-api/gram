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

func TestClaudeSessionSource_PreservesAmbiguousSourceWithoutVariant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)

	metadata := &SessionMetadata{
		SessionID:           uuid.NewString(),
		ServiceName:         "claude",
		UserEmail:           "",
		UserID:              "",
		Provider:            "",
		ExternalOrgID:       "",
		ExternalAccountUUID: "",
		ExternalAccountID:   "",
		DeviceID:            "",
		Hostname:            "",
		AccountType:         "",
		BillingMode:         "",
		UserAccountID:       "",
		ObservedUserEmail:   "",
		GramOrgID:           "",
		ProjectID:           "",
	}

	require.Equal(t, "claude", ti.service.claudeSessionSource(ctx, metadata))
}

// TestCoworkHookSource_ConsistentAcrossAllWrites asserts that the product
// surface detected from SessionStart wins over the ambiguous service.name on
// every chat_messages write path. The chat list infers a session's source from
// these rows, so one path falling back to "claude" would mislabel the session.
func TestCoworkHookSource_ConsistentAcrossAllWrites(t *testing.T) {
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
	require.NoError(t, ti.service.cache.Set(ctx, sessionAgentVariantCacheKey(sessionID),
		agentVariantCowork, sessionMCPListTTL))
	const wantSource = "cowork"
	const wantUserID = "session-capture-user"
	const wantUserEmail = "tester@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, wantUserID, wantUserEmail)

	metadata := &SessionMetadata{
		SessionID:     sessionID,
		ServiceName:   "claude",
		UserEmail:     wantUserEmail,
		UserID:        wantUserID,
		ExternalOrgID: "",
		GramOrgID:     authCtx.ActiveOrganizationID,
		ProjectID:     authCtx.ProjectID.String(),
	}

	toolUseID := "toolu_consistency_" + uuid.NewString()
	toolName := "Edit"
	model := "claude-opus"
	prompt := "hello"
	lastAssistantMessage := "ok"
	toolResponse := map[string]any{"ok": true}
	errorData := map[string]any{"message": "boom"}

	// Each of these is a distinct write path. They must all consult the cached
	// variant rather than persisting the ambiguous service.name.
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
			"Source should match the detected Cowork variant for all hook writes (role=%s)", m.Role)
		assert.True(t, m.UserID.Valid, "UserID should be set (role=%s)", m.Role)
		assert.Equal(t, wantUserID, m.UserID.String,
			"UserID should match metadata.UserID for all hook writes (role=%s)", m.Role)
	}
}

func TestClaudeUserPromptSubmitDoesNotPersistPromptIDAsMessageID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

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
		SessionID:     sessionID,
		ServiceName:   "claude-code",
		UserEmail:     wantUserEmail,
		UserID:        wantUserID,
		ExternalOrgID: "",
		GramOrgID:     authCtx.ActiveOrganizationID,
		ProjectID:     authCtx.ProjectID.String(),
	}
	prompt := "hello"

	require.NoError(t, ti.service.persistConversationEvent(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
		AdditionalData: map[string]any{
			"promptId": wantPromptID,
		},
	}, metadata))

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
	ti.service.productFeatures = alwaysEnabledFeatures{}

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
		SessionID:     sessionID,
		ServiceName:   "claude-code",
		UserEmail:     wantUserEmail,
		UserID:        wantUserID,
		ExternalOrgID: "",
		GramOrgID:     authCtx.ActiveOrganizationID,
		ProjectID:     authCtx.ProjectID.String(),
	}
	prompt := "hello"
	lastAssistantMessage := "ok"

	require.NoError(t, ti.service.persistConversationEvent(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
		AdditionalData: map[string]any{
			"promptId": stalePromptID,
		},
	}, metadata))

	msgs, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, "user", msgs[0].Role)
	require.False(t, msgs[0].MessageID.Valid, "UserPromptSubmit promptId must not be persisted directly")

	require.NoError(t, ti.service.persistConversationEvent(ctx, &gen.ClaudePayload{
		HookEventName:        "Stop",
		SessionID:            &sessionID,
		LastAssistantMessage: &lastAssistantMessage,
		AdditionalData: map[string]any{
			"LastUserPromptID": wantPromptID,
		},
	}, metadata))

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
