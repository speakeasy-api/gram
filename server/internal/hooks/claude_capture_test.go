package hooks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskRepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

type failingFeatures struct{}

func (failingFeatures) IsFeatureEnabled(_ context.Context, _ string, _ productfeatures.Feature) (bool, error) {
	return false, errors.New("feature lookup unavailable")
}

type blockingPromptScanner struct{}

func (blockingPromptScanner) ScanForEnforcement(_ context.Context, _ string, _ uuid.UUID, _ string, _ string, _ string, _ string) (*risk.ScanResult, error) {
	return &risk.ScanResult{PolicyName: "blocked-prompt", Description: "prompt matched test policy"}, nil
}

func (blockingPromptScanner) LookupShadowMCPBlockingPolicy(_ context.Context, _ string, _ uuid.UUID, _ string) (*risk.ShadowMCPPolicy, error) {
	return nil, nil
}

func (blockingPromptScanner) HasEnabledShadowMCPPolicy(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}

// seedCaptureSession wires up an attributed Claude session for capture tests:
// a connected user plus cached session metadata so resolveClaudeSessionMetadata
// resolves the project/org.
func seedCaptureSession(t *testing.T, ctx context.Context, ti *testInstance, sessionID, userID, userEmail string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, userID, userEmail)
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		UserEmail:   userEmail,
		UserID:      userID,
		ClaudeOrgID: authCtx.ActiveOrganizationID,
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}, time.Hour))
}

// TestClaudeMessages_DeliveredTwiceStoredOnce is the core dedup guarantee: two
// plugin installations firing the same Stop send the same transcript-uuid batch,
// and the server stores each message exactly once (ON CONFLICT on
// external_message_id).
func TestClaudeMessages_DeliveredTwiceStoredOnce(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	seedCaptureSession(t, ctx, ti, sessionID, "capture-user", "capture@example.com")

	userContent := "hello from the user"
	asstContent := "the assistant reply"
	model := "claude-opus-4-8"
	batch := func() *gen.ClaudeMessagesPayload {
		return &gen.ClaudeMessagesPayload{
			SessionID: sessionID,
			Messages: []*gen.ClaudeCapturedMessage{
				{ExternalID: uuid.NewString(), Role: "user", Content: &userContent},
				{ExternalID: uuid.NewString(), Role: "assistant", Content: &asstContent, Model: &model},
			},
		}
	}
	// Both installs observe the same transcript, so the external_ids are identical
	// across deliveries — reuse one batch value rather than minting new uuids.
	payload := batch()

	require.NoError(t, ti.service.ClaudeMessages(ctx, payload))

	var msgs []chatRepo.ChatMessage
	require.Eventually(t, func() bool {
		var err error
		msgs, err = chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(msgs) == 2
	}, 2*time.Second, 25*time.Millisecond, "first batch should persist two rows")

	// Second plugin install fires the identical batch.
	require.NoError(t, ti.service.ClaudeMessages(ctx, payload))

	require.Never(t, func() bool {
		got, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(got) != 2
	}, 500*time.Millisecond, 50*time.Millisecond, "re-delivery must dedup on external_message_id")

	for _, msg := range msgs {
		require.True(t, msg.ExternalMessageID.Valid, "captured messages must carry external_message_id for dedup")
	}
}

func TestClaudeMessages_BufferedUntilSessionMetadataResolves(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	userContent := "captured after metadata arrives"
	payload := &gen.ClaudeMessagesPayload{
		SessionID: sessionID,
		Messages: []*gen.ClaudeCapturedMessage{
			{ExternalID: uuid.NewString(), Role: "user", Content: &userContent},
		},
	}

	require.NoError(t, ti.service.ClaudeMessages(otelOnlyCtx(ctx), payload))

	var buffered []gen.ClaudeMessagesPayload
	require.NoError(t, ti.service.cache.ListRange(ctx, claudeMessagesPendingCacheKey(sessionID), 0, -1, &buffered))
	require.Len(t, buffered, 1, "batch should be buffered while metadata is unavailable")

	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "buffered-capture-user", "buffered-capture@example.com")
	metadata := SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		UserEmail:   "buffered-capture@example.com",
		UserID:      "buffered-capture-user",
		ClaudeOrgID: authCtx.ActiveOrganizationID,
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}
	require.NoError(t, ti.service.cache.Set(ctx, sessionCacheKey(sessionID), metadata, time.Hour))

	ti.service.flushPendingHooks(ctx, sessionID, &metadata)

	require.Eventually(t, func() bool {
		got, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(got) == 1 && got[0].Content == userContent
	}, 2*time.Second, 25*time.Millisecond, "buffered batch should persist after metadata is cached")

	var after []gen.ClaudeMessagesPayload
	require.NoError(t, ti.service.cache.ListRange(ctx, claudeMessagesPendingCacheKey(sessionID), 0, -1, &after))
	require.Empty(t, after, "buffer should be deleted after flush")
}

func TestClaudeMessages_UpdatesChatAttributionAfterInitialUnauthenticatedCapture(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	userContent := "captured before attribution"
	payload := &gen.ClaudeMessagesPayload{
		SessionID: sessionID,
		Messages: []*gen.ClaudeCapturedMessage{
			{ExternalID: uuid.NewString(), Role: "user", Content: &userContent},
		},
	}
	baseMetadata := SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		UserEmail:   "",
		UserID:      "",
		ClaudeOrgID: authCtx.ActiveOrganizationID,
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}
	require.NoError(t, ti.service.persistClaudeMessages(ctx, payload, baseMetadata, ti.service.logger))

	attributedMetadata := baseMetadata
	attributedMetadata.UserEmail = "attributed@example.com"
	attributedMetadata.UserID = "attributed-user"
	seedHookUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, attributedMetadata.UserID, attributedMetadata.UserEmail)
	require.NoError(t, ti.service.persistClaudeMessages(ctx, payload, attributedMetadata, ti.service.logger))

	chat, err := chatRepo.New(ti.conn).GetChat(ctx, chatID)
	require.NoError(t, err)
	require.Equal(t, attributedMetadata.UserID, chat.UserID.String)
	require.True(t, chat.UserID.Valid)
	require.Equal(t, attributedMetadata.UserEmail, chat.ExternalUserID.String)
	require.True(t, chat.ExternalUserID.Valid)
}

func TestClaudeMessages_PendingBufferSurvivesReplayFailure(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = failingFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	userContent := "keep me buffered"
	payload := &gen.ClaudeMessagesPayload{
		SessionID: sessionID,
		Messages: []*gen.ClaudeCapturedMessage{
			{ExternalID: uuid.NewString(), Role: "user", Content: &userContent},
		},
	}
	require.NoError(t, ti.service.bufferClaudeMessages(ctx, sessionID, payload))

	metadata := SessionMetadata{
		SessionID:   sessionID,
		ServiceName: "claude-code",
		UserEmail:   "buffered-capture@example.com",
		UserID:      "buffered-capture-user",
		ClaudeOrgID: authCtx.ActiveOrganizationID,
		GramOrgID:   authCtx.ActiveOrganizationID,
		ProjectID:   authCtx.ProjectID.String(),
	}

	ti.service.flushPendingClaudeMessages(ctx, sessionID, &metadata)

	var buffered []gen.ClaudeMessagesPayload
	require.NoError(t, ti.service.cache.ListRange(ctx, claudeMessagesPendingCacheKey(sessionID), 0, -1, &buffered))
	require.Len(t, buffered, 1, "failed replay must keep the pending batch for a later flush")
}

func TestClaudeMessages_SkipsToolMessageWithoutToolCallID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	seedCaptureSession(t, ctx, ti, sessionID, "tool-user", "tool@example.com")

	content := "orphaned tool result"
	require.NoError(t, ti.service.ClaudeMessages(ctx, &gen.ClaudeMessagesPayload{
		SessionID: sessionID,
		Messages: []*gen.ClaudeCapturedMessage{
			{ExternalID: uuid.NewString(), Role: "tool", Content: &content},
		},
	}))

	require.Never(t, func() bool {
		got, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(got) > 0
	}, 500*time.Millisecond, 50*time.Millisecond, "tool messages without tool_call_id must be skipped")
}

func TestClaudeMessages_FlushesPendingShadowMCPBlockFinding(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	policyID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	_, err := riskRepo.New(ti.conn).CreateRiskPolicy(ctx, riskRepo.CreateRiskPolicyParams{
		ID:             policyID,
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           "shadow-mcp-block",
		Sources:        []string{"shadow_mcp"},
		Enabled:        true,
		Action:         "block",
		AudienceType:   "everyone",
		AutoName:       false,
	})
	require.NoError(t, err)

	sessionID := uuid.NewString()
	toolName := "mcp__mise__install_tool"
	toolUseID := "toolu_pending_shadow_mcp"
	userEmail := "claude-shadow-buffer@example.com"
	seedCaptureSession(t, ctx, ti, sessionID, "shadow-buffer-user", userEmail)
	require.NoError(t, ti.service.cache.Set(ctx, sessionMCPListCacheKey(sessionID),
		[]MCPServerEntry{{Source: "local", Name: "mise", Command: "mise mcp", Transport: "STDIO"}},
		sessionMCPListTTL,
	))

	version := claudeHookStopCollectionVersion
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "PreToolUse",
		SessionID:     &sessionID,
		UserEmail:     &userEmail,
		ToolName:      &toolName,
		ToolUseID:     &toolUseID,
		ToolInput:     map[string]any{"package": "node"},
		HookVersion:   &version,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	var pending []pendingShadowMCPBlockFinding
	require.Eventually(t, func() bool {
		pending = nil
		err := ti.service.cache.ListRange(ctx, shadowMCPBlockFindingsPendingCacheKey(sessionID), 0, -1, &pending)
		return err == nil && len(pending) == 1
	}, 500*time.Millisecond, 50*time.Millisecond)
	require.Equal(t, toolUseID, pending[0].ToolCallID)
	require.Equal(t, policyID.String(), pending[0].PolicyID)

	assistantContent := ""
	require.NoError(t, ti.service.ClaudeMessages(ctx, &gen.ClaudeMessagesPayload{
		SessionID: sessionID,
		Messages: []*gen.ClaudeCapturedMessage{{
			ExternalID: uuid.NewString(),
			Role:       "assistant",
			Content:    &assistantContent,
			ToolCalls: []any{map[string]any{
				"id":   toolUseID,
				"type": "function",
				"function": map[string]any{
					"name":      toolName,
					"arguments": `{"package":"node"}`,
				},
			}},
		}},
	}))

	require.Eventually(t, func() bool {
		_, err := ti.service.repo.FindAssistantToolCallMessageID(ctx, repo.FindAssistantToolCallMessageIDParams{
			ProjectID:  uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
			ChatID:     sessionIDToUUID(sessionID),
			ToolCallID: toolUseID,
		})
		return err == nil
	}, time.Second, 50*time.Millisecond)

	require.Eventually(t, func() bool {
		got, err := riskRepo.New(ti.conn).CountRiskResultsByPolicyID(ctx, riskRepo.CountRiskResultsByPolicyIDParams{
			RiskPolicyID: policyID,
			ProjectID:    *authCtx.ProjectID,
		})
		return err == nil && got >= 1
	}, time.Second, 50*time.Millisecond)

	pending = nil
	require.NoError(t, ti.service.cache.ListRange(ctx, shadowMCPBlockFindingsPendingCacheKey(sessionID), 0, -1, &pending))
	require.Empty(t, pending)
}

func TestClaudeMessages_MergesSubagentMessagesIntoParentStopOrder(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	seedCaptureSession(t, ctx, ti, sessionID, "subagent-user", "subagent@example.com")

	agentID := "agent-1"
	agentType := "general-purpose"
	subContent := "subagent tool request"
	subTS := "2026-06-26T09:00:02Z"
	require.NoError(t, ti.service.ClaudeMessages(ctx, &gen.ClaudeMessagesPayload{
		SessionID: sessionID,
		Messages: []*gen.ClaudeCapturedMessage{{
			ExternalID: "subagent-tool-call",
			Role:       "assistant",
			Content:    &subContent,
			AgentID:    &agentID,
			AgentType:  &agentType,
			Timestamp:  &subTS,
			ToolCalls: []any{map[string]any{
				"id":   "toolu_subagent",
				"type": "function",
				"function": map[string]any{
					"name":      "Read",
					"arguments": `{"file_path":"README.md"}`,
				},
			}},
		}},
	}))

	require.Never(t, func() bool {
		got, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(got) > 0
	}, 300*time.Millisecond, 50*time.Millisecond, "SubagentStop must wait for parent Stop before inserting")

	userContent := "parent prompt"
	assistantContent := "parent response"
	userTS := "2026-06-26T09:00:01Z"
	assistantTS := "2026-06-26T09:00:03Z"
	require.NoError(t, ti.service.ClaudeMessages(ctx, &gen.ClaudeMessagesPayload{
		SessionID: sessionID,
		Messages: []*gen.ClaudeCapturedMessage{
			{ExternalID: "parent-user", Role: "user", Content: &userContent, Timestamp: &userTS},
			{ExternalID: "parent-assistant", Role: "assistant", Content: &assistantContent, Timestamp: &assistantTS},
		},
	}))

	var msgs []chatRepo.ChatMessage
	require.Eventually(t, func() bool {
		var err error
		msgs, err = chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(msgs) == 3
	}, time.Second, 50*time.Millisecond)
	require.Equal(t, "parent-user", msgs[0].ExternalMessageID.String)
	require.Equal(t, "subagent-tool-call", msgs[1].ExternalMessageID.String)
	require.Equal(t, "parent-assistant", msgs[2].ExternalMessageID.String)

	var pending []gen.ClaudeMessagesPayload
	require.NoError(t, ti.service.cache.ListRange(ctx, claudeSubagentMessagesPendingCacheKey(sessionID), 0, -1, &pending))
	require.Empty(t, pending)
}

// TestClaudeHookVersion_StopCollectionSkipsPerEventPersist verifies the version
// gate: a post-Stop-collection plugin (sends X-Gram-Hook-Version) must NOT
// persist chat_messages on the per-event handlers — capture comes from the Stop
// batch instead, so persisting here would double-write.
func TestClaudeHookVersion_StopCollectionSkipsPerEventPersist(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	seedCaptureSession(t, ctx, ti, sessionID, "gate-user", "gate@example.com")

	version := claudeHookStopCollectionVersion
	prompt := "should not persist on the per-event handler"
	_, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
		HookVersion:   &version,
	})
	require.NoError(t, err)

	require.Never(t, func() bool {
		got, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(got) > 0
	}, 500*time.Millisecond, 50*time.Millisecond, "Stop-collection UserPromptSubmit must not persist per-event")
}

func TestClaudeHookVersion_BlockedPromptPersistsWithoutStopBatch(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = blockingPromptScanner{}

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	sessionID := uuid.NewString()
	chatID := sessionIDToUUID(sessionID)
	seedCaptureSession(t, ctx, ti, sessionID, "blocked-prompt-user", "blocked-prompt@example.com")

	version := claudeHookStopCollectionVersion
	prompt := "this prompt should be blocked and still captured"
	result, err := ti.service.Claude(ctx, &gen.ClaudePayload{
		HookEventName: "UserPromptSubmit",
		SessionID:     &sessionID,
		Prompt:        &prompt,
		HookVersion:   &version,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Decision)
	require.Equal(t, "block", *result.Decision)

	require.Eventually(t, func() bool {
		got, err := chatRepo.New(ti.conn).ListChatMessages(ctx, chatRepo.ListChatMessagesParams{
			ChatID:    chatID,
			ProjectID: *authCtx.ProjectID,
		})
		return err == nil && len(got) == 1 && got[0].Content == prompt
	}, time.Second, 50*time.Millisecond)
}
