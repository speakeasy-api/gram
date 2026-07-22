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
	"github.com/speakeasy-api/gram/server/internal/hookevents"
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
	const wantUserID = "session-capture-user"
	const wantUserEmail = "tester@example.com"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, wantUserID, wantUserEmail)

	metadata := &SessionMetadata{
		SessionID:     sessionID,
		ServiceName:   wantSource,
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

// The three Claude surfaces must resolve distinctly: cowork self-identifies
// on the OTEL service.name, Claude Code Desktop on the desktop hook adapter
// slug, and the CLI on the shared "claude-code" name. Non-Claude values must
// not resolve at all.
func TestClaudeSurfaceFromServiceName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "cowork", claudeSurfaceFromServiceName("cowork"))
	assert.Equal(t, "cowork", claudeSurfaceFromServiceName("claude-cowork"))
	assert.Equal(t, "cowork", claudeSurfaceFromServiceName(" Claude Cowork "))
	assert.Equal(t, "claude-code-desktop", claudeSurfaceFromServiceName("claude-code-desktop"))
	assert.Equal(t, "claude-code", claudeSurfaceFromServiceName("claude-code"))
	assert.Equal(t, "claude-code", claudeSurfaceFromServiceName("ClaudeCode"))
	assert.Empty(t, claudeSurfaceFromServiceName("cursor"))
	assert.Empty(t, claudeSurfaceFromServiceName("codex_cli_rs"))
	assert.Empty(t, claudeSurfaceFromServiceName(""))
}

// Merging a fresh service name with the cached one keeps whichever pins the
// surface more precisely: "cowork" beats the desktop adapter slug, the
// desktop adapter slug beats the ambiguous "claude-code" the OTEL stream
// reports for both desktop and CLI, and ties keep the fresh value.
func TestPreferClaudeServiceName(t *testing.T) {
	t.Parallel()

	// OTEL "cowork" upgrades a cached desktop adapter slug.
	assert.Equal(t, "cowork", preferClaudeServiceName("cowork", "claude-code-desktop"))
	// A cached "cowork" survives both the ambiguous OTEL name and the shared
	// desktop adapter slug.
	assert.Equal(t, "cowork", preferClaudeServiceName("claude-code", "cowork"))
	assert.Equal(t, "cowork", preferClaudeServiceName("claude-code-desktop", "cowork"))
	// A cached desktop adapter slug survives OTEL batches reporting the
	// ambiguous "claude-code"; the adapter upgrades a cached ambiguous name.
	assert.Equal(t, "claude-code-desktop", preferClaudeServiceName("claude-code", "claude-code-desktop"))
	assert.Equal(t, "claude-code-desktop", preferClaudeServiceName("claude-code-desktop", "claude-code"))
	// Empty incoming keeps the cache; empty cache takes the incoming value.
	assert.Equal(t, "cowork", preferClaudeServiceName("", "cowork"))
	assert.Equal(t, "claude-code", preferClaudeServiceName("claude-code", ""))
	// Non-Claude values tie at zero specificity: fresh wins.
	assert.Equal(t, "cursor", preferClaudeServiceName("cursor", "Cursor"))
}

// A session whose OTEL stream reports service.name "cowork" must persist its
// chat messages with source "cowork" — no SessionStart inventory variant
// needed. This is the current cowork identification path: the canonical
// ingest transport stamps no variant, so the service name is the only signal.
func TestClaudeChatSource_CoworkFromServiceName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	prompt := "hello from cowork"

	metadata := &SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "cowork",
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
	require.Len(t, msgs, 1)
	require.True(t, msgs[0].Source.Valid)
	require.Equal(t, "cowork", msgs[0].Source.String)
}

// Older cowork builds report service.name "claude-code"; for those the
// inventory-shape variant stamped at SessionStart must still relabel chat
// messages as cowork.
func TestClaudeChatSource_CoworkFromVariantOverridesAmbiguousServiceName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	prompt := "hello from legacy cowork"
	require.NoError(t, ti.service.cache.Set(ctx, sessionAgentVariantCacheKey(sessionID),
		agentVariantCowork, sessionMCPListTTL))

	metadata := &SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
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
	require.Len(t, msgs, 1)
	require.True(t, msgs[0].Source.Valid)
	require.Equal(t, "cowork", msgs[0].Source.String)
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

func TestClaudeSessionEndDoesNotWakeBeforeTranscriptPersistence(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	projectID := uuid.New()

	unattributed, err := ti.service.handleSessionEnd(ctx, hookevents.NewSessionEnd(
		hookevents.Event{
			Provider: "claude", Type: "", RawEventType: "SessionEnd", Timestamp: time.Now(),
			AuthContext: nil, ConversationID: "unattributed", Raw: nil,
			Context: hookevents.EventContext{
				OrganizationID: "", ProjectID: uuid.Nil, User: hookevents.User{ID: "", Email: ""},
			},
		},
		hookevents.SessionEndParams{Reason: "clear"},
	))
	require.NoError(t, err)
	require.NotNil(t, unattributed)
	require.Empty(t, ti.efficacySignals.signaled(), "a session with no project wakes nothing")

	resolved, err := ti.service.handleSessionEnd(ctx, hookevents.NewSessionEnd(
		hookevents.Event{
			Provider: "claude", Type: "", RawEventType: "SessionEnd", Timestamp: time.Now(),
			AuthContext: nil, ConversationID: "resolved", Raw: nil,
			Context: hookevents.EventContext{
				OrganizationID: "org", ProjectID: projectID, User: hookevents.User{ID: "", Email: ""},
			},
		},
		hookevents.SessionEndParams{Reason: "exit"},
	))
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.Empty(t, ti.efficacySignals.signaled(), "durable observations, messages, and the sweep provide later wakes")
}
