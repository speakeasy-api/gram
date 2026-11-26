package chat_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func TestService_SummarizeToolCall_GeneratesAndCachesSeparately(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetCompletion", mock.Anything, mock.MatchedBy(func(request openrouter.CompletionRequest) bool {
		return len(request.Messages) == 2 && request.Model == "google/gemini-3.1-flash-lite"
	})).Return(assistantTextResponse(`{"summary":"Looked up the account in the CRM. The account was found and returned.","impact":"read_only"}`), nil).Once()

	ti := newTestChatServiceWithCompletion(t, client)
	ctx := grantOrgAdminWithChatRead(t, initSessionCtx(t, ti))
	chatID := seedChat(t, ctx, ti, "", "ext-user", "Tool summary")
	toolCallID := "call_test"

	queries := repo.New(ti.conn)
	messageID, err := queries.CreateChatMessageReturningID(ctx, repo.CreateChatMessageReturningIDParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true}, Role: "assistant", Content: "",
		ToolCalls:  []byte(`[{"id":"call_test","type":"function","function":{"name":"lookup_account","arguments":{"account_id":"placeholder"}}}]`),
		ToolCallID: pgtype.Text{},
	})
	require.NoError(t, err)

	_, err = queries.CreateChatMessageReturningID(ctx, repo.CreateChatMessageReturningIDParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true}, Role: "tool", Content: "Account found",
		ToolCalls: nil, ToolCallID: pgtype.Text{String: toolCallID, Valid: true},
	})
	require.NoError(t, err)

	payload := &gen.SummarizeToolCallPayload{
		ID: chatID.String(), MessageID: messageID.String(), ToolCallID: toolCallID,
	}
	first, err := ti.service.SummarizeToolCall(ctx, payload)
	require.NoError(t, err)
	require.False(t, first.Cached)
	require.Contains(t, first.Summary, "Looked up")
	require.Equal(t, "read_only", first.Impact)

	second, err := ti.service.SummarizeToolCall(ctx, payload)
	require.NoError(t, err)
	require.True(t, second.Cached)
	require.Equal(t, first.Summary, second.Summary)
	require.Equal(t, first.Impact, second.Impact)
	client.AssertExpectations(t)
}

// A summary is part of the message body, so storing one reseals the encrypted
// transcript in the same transaction: the next load still opens the message
// from its transcript, with no plaintext fallback.
func TestService_SummarizeToolCall_ResealsTheTranscript(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetCompletion", mock.Anything, mock.Anything).Return(assistantTextResponse(`{"summary":"Looked up the account.","impact":"read_only"}`), nil).Once()

	ti := newTestChatServiceWithCompletion(t, client)
	ctx := grantOrgAdminWithChatRead(t, initSessionCtx(t, ti))
	chatID := seedChat(t, ctx, ti, "", "ext-user", "Sealed tool summary")

	queries := repo.New(ti.conn)
	messageID, err := queries.CreateChatMessageReturningID(ctx, repo.CreateChatMessageReturningIDParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true}, Role: "assistant", Content: "",
		ToolCalls:  []byte(`[{"id":"call_test","type":"function","function":{"name":"lookup_account","arguments":{}}}]`),
		ToolCallID: pgtype.Text{},
	})
	require.NoError(t, err)
	result, err := queries.CreateChatMessageReturningID(ctx, repo.CreateChatMessageReturningIDParams{
		ChatID: chatID, ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true}, Role: "tool", Content: "Account found",
		ToolCalls: nil, ToolCallID: pgtype.Text{String: "call_test", Valid: true},
	})
	require.NoError(t, err)

	store, reader := newMeteredTranscriptShadowStore(t, ti)
	captureTranscriptShadows(t, ti, store, messageID, result)
	ti.service.WithTranscripts(transcriptstore.NewTranscripts(store, ti.projectID))

	_, err = ti.service.SummarizeToolCall(ctx, &gen.SummarizeToolCallPayload{
		ID: chatID.String(), MessageID: messageID.String(), ToolCallID: "call_test",
	})
	require.NoError(t, err)

	opened, err := store.Load(ctx, ti.orgID, ti.projectID, messageID)
	require.NoError(t, err)
	require.Contains(t, string(opened.ToolCallSummaries), "Looked up the account.")
	_, err = ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"success": 2}, readMessagesByOutcome(t, reader))
	client.AssertExpectations(t)
}
