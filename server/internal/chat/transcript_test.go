package chat_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	telemetryv1 "github.com/speakeasy-api/gram/infra/gen/gram/telemetry/v1"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

const transcriptTestEmail = "dedup@example.test"

func toolCallJSON(id string) []byte {
	return []byte(`[{"id":"` + id + `","type":"function","function":{"name":"lookup","arguments":"{\"city\":\"Paris\"}"}}]`)
}

// hookWrite is a row as Claude Code hooks deliver it: the chat is the session.
func hookWrite(chatID, projectID uuid.UUID, userID, sessionID, role, content string, toolCalls []byte, toolCallID string) chat.MessageWrite {
	return chat.MessageWrite{
		Params: repo.CreateChatMessageParams{
			ID:             uuid.Nil,
			ChatID:         chatID,
			ProjectID:      projectID,
			Role:           role,
			Content:        content,
			ToolCalls:      toolCalls,
			ToolCallID:     conv.ToPGTextEmpty(toolCallID),
			UserID:         conv.ToPGText(userID),
			ExternalUserID: conv.ToPGText(transcriptTestEmail),
			Source:         conv.ToPGText("claude-code"),
		},
		BillingUserID: userID,
		UserEmail:     transcriptTestEmail,
		Provider:      "anthropic",
		HookHostname:  "workstation.example.test",
		AccountType:   "team",
		BillingMode:   "metered",
		SessionID:     sessionID,
	}
}

// inferenceWrite is a row as the Anthropic inference hook delivers it: its own
// chat, but the same session id and user as the hooks chat.
func inferenceWrite(chatID, projectID uuid.UUID, userID, sessionID, role, content string, toolCalls []byte, toolCallID string) chat.ExternalMessageWrite {
	return chat.ExternalMessageWrite{
		Params: repo.CreateExternalChatMessageParams{
			ID:                uuid.Nil,
			ChatID:            chatID,
			ProjectID:         projectID,
			Role:              role,
			Content:           content,
			ToolCalls:         toolCalls,
			ToolCallID:        conv.ToPGTextEmpty(toolCallID),
			UserID:            conv.ToPGText(userID),
			ExternalUserID:    conv.ToPGText(transcriptTestEmail),
			ExternalMessageID: conv.ToPGText("anthropic-inference:" + uuid.NewString()),
			Origin:            conv.ToPGText("anthropic-inference"),
			Source:            conv.ToPGText("claude-code-web"),
		},
		BillingUserID: userID,
		UserEmail:     transcriptTestEmail,
		Provider:      "anthropic",
		AccountType:   "team",
		BillingMode:   "unknown",
		SessionID:     sessionID,
	}
}

func sessionObservations(t *testing.T, ti *chatTestInstance) int {
	t.Helper()
	rows, err := testrepo.New(ti.conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	count := 0
	for _, row := range rows {
		if row.Topic == string(proto.MessageName(&telemetryv1.SessionObserved{})) {
			count++
		}
	}
	return count
}

func newTranscriptWriter(t *testing.T, ti *chatTestInstance) *chat.ChatMessageWriter {
	t.Helper()
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	return writer
}

func TestTranscriptStoresSecondSourceOfToolCallAsSecondary(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	writer := newTranscriptWriter(t, ti)
	userID := uuid.NewString()
	hooksChat := seedChat(t, ctx, ti, userID, transcriptTestEmail, "hooks chat")
	sessionID := hooksChat.String()
	inferenceChat := seedChat(t, ctx, ti, userID, transcriptTestEmail, "inference chat")

	written, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{
		hookWrite(hooksChat, ti.projectID, userID, sessionID, "assistant", "", toolCallJSON("toolu_01A"), ""),
		hookWrite(hooksChat, ti.projectID, userID, sessionID, "tool", `{"weather":"sunny"}`, nil, "toolu_01A"),
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), written)
	readingsAfterHooks := len(meterMessages(t, ti))
	eventsAfterHooks := len(conversationMessages(t, ti))
	require.Equal(t, 2, readingsAfterHooks)
	require.Equal(t, 2, eventsAfterHooks)

	written, err = writer.WriteExternalWithContentParts(ctx, ti.projectID, []chat.ExternalMessageWrite{
		inferenceWrite(inferenceChat, ti.projectID, userID, sessionID, "assistant", "", toolCallJSON("toolu_01A"), ""),
		inferenceWrite(inferenceChat, ti.projectID, userID, sessionID, "tool", `{"weather":"sunny"}`, nil, "toolu_01A"),
	}, nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), written)

	primaries := listAllMessages(t, ctx, ti.conn, hooksChat, ti.projectID)
	require.Len(t, primaries, 2)
	for _, row := range primaries {
		require.False(t, row.PrimaryMessageID.Valid, "the first source stays primary")
		require.False(t, row.RiskAnalyzedAt.Valid)
	}
	secondaries := listAllMessages(t, ctx, ti.conn, inferenceChat, ti.projectID)
	require.Len(t, secondaries, 2)
	for i, row := range secondaries {
		require.True(t, row.PrimaryMessageID.Valid, "row %d must be a secondary", i)
		require.Equal(t, primaries[i].ID, row.PrimaryMessageID.UUID)
		require.True(t, row.RiskAnalyzedAt.Valid, "a secondary is never swept for risk analysis")
	}

	require.Len(t, meterMessages(t, ti), readingsAfterHooks, "a secondary is not metered")
	require.Len(t, conversationMessages(t, ti), eventsAfterHooks, "a secondary is not announced")
	require.Equal(t, 0, sessionObservations(t, ti), "a secondary does not establish a second session")

	keys, err := repo.New(ti.conn).LookupChatEntryKeys(ctx, repo.LookupChatEntryKeysParams{
		ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
		Keys:      []string{"tool-call:toolu_01A", "tool-result:toolu_01A"},
	})
	require.NoError(t, err)
	require.Len(t, keys, 2)
	for _, key := range keys {
		require.Equal(t, hooksChat, key.ChatID, "keys belong to the primary's chat")
	}
}

func TestTranscriptNeverLinksAnotherUsersRows(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	writer := newTranscriptWriter(t, ti)
	ownerID, otherID := uuid.NewString(), uuid.NewString()
	ownerChat := seedChat(t, ctx, ti, ownerID, transcriptTestEmail, "owner chat")
	otherChat := seedChat(t, ctx, ti, otherID, "other@example.test", "other chat")

	_, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{
		hookWrite(ownerChat, ti.projectID, ownerID, ownerChat.String(), "tool", "owner result", nil, "toolu_01B"),
	})
	require.NoError(t, err)

	// A client-asserted session id and a stolen tool id must not attach the
	// other user's row to the owner's entry.
	forged := hookWrite(otherChat, ti.projectID, otherID, ownerChat.String(), "tool", "forged result", nil, "toolu_01B")
	forged.UserEmail = "other@example.test"
	forged.Params.ExternalUserID = conv.ToPGText("other@example.test")
	_, err = writer.Write(ctx, ti.projectID, []chat.MessageWrite{forged})
	require.NoError(t, err)

	rows := listAllMessages(t, ctx, ti.conn, otherChat, ti.projectID)
	require.Len(t, rows, 1)
	require.False(t, rows[0].PrimaryMessageID.Valid)
	require.False(t, rows[0].RiskAnalyzedAt.Valid)
	require.Len(t, meterMessages(t, ti), 2, "both users' rows are metered")

	keys, err := repo.New(ti.conn).LookupChatEntryKeys(ctx, repo.LookupChatEntryKeysParams{
		ProjectID: uuid.NullUUID{UUID: ti.projectID, Valid: true},
		Keys:      []string{"tool-result:toolu_01B"},
	})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, ownerChat, keys[0].ChatID, "the key stays with its first owner")
}

func TestTranscriptMatchesRepliesInTheOpenSegment(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	writer := newTranscriptWriter(t, ti)
	userID := uuid.NewString()
	hooksChat := seedChat(t, ctx, ti, userID, transcriptTestEmail, "hooks chat")
	sessionID := hooksChat.String()
	inferenceChat := seedChat(t, ctx, ti, userID, transcriptTestEmail, "inference chat")

	_, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{
		hookWrite(hooksChat, ti.projectID, userID, sessionID, "user", "What is the weather?", nil, ""),
		hookWrite(hooksChat, ti.projectID, userID, sessionID, "assistant", "", toolCallJSON("toolu_01C"), ""),
		hookWrite(hooksChat, ti.projectID, userID, sessionID, "tool", `{"weather":"sunny"}`, nil, "toolu_01C"),
		hookWrite(hooksChat, ti.projectID, userID, sessionID, "assistant", "It is sunny in Paris.", nil, ""),
	})
	require.NoError(t, err)
	primaries := listAllMessages(t, ctx, ti.conn, hooksChat, ti.projectID)
	require.Len(t, primaries, 4)

	// The inference copy carries the tool result (keyed), the same reply with
	// different whitespace (unkeyed, inside the open segment after the tool
	// result) and a reply hooks never saw.
	_, err = writer.WriteExternalWithContentParts(ctx, ti.projectID, []chat.ExternalMessageWrite{
		inferenceWrite(inferenceChat, ti.projectID, userID, sessionID, "tool", `{"weather":"sunny"}`, nil, "toolu_01C"),
		inferenceWrite(inferenceChat, ti.projectID, userID, sessionID, "assistant", "  It is sunny in Paris.\n", nil, ""),
		inferenceWrite(inferenceChat, ti.projectID, userID, sessionID, "assistant", "Anything else?", nil, ""),
	}, nil)
	require.NoError(t, err)

	rows := listAllMessages(t, ctx, ti.conn, inferenceChat, ti.projectID)
	require.Len(t, rows, 3)
	require.True(t, rows[0].PrimaryMessageID.Valid)
	require.Equal(t, primaries[2].ID, rows[0].PrimaryMessageID.UUID, "tool result links by key")
	require.True(t, rows[1].PrimaryMessageID.Valid)
	require.Equal(t, primaries[3].ID, rows[1].PrimaryMessageID.UUID, "reply links by text inside the open segment")
	require.False(t, rows[2].PrimaryMessageID.Valid, "a reply hooks never stored stays primary")
	require.False(t, rows[2].RiskAnalyzedAt.Valid)

	// Text before the shared tool call is outside the open segment: an
	// identical prompt there is a new entry, never a match.
	_, err = writer.WriteExternalWithContentParts(ctx, ti.projectID, []chat.ExternalMessageWrite{
		inferenceWrite(inferenceChat, ti.projectID, userID, sessionID, "user", "What is the weather?", nil, ""),
	}, nil)
	require.NoError(t, err)
	rows = listAllMessages(t, ctx, ti.conn, inferenceChat, ti.projectID)
	require.Len(t, rows, 4)
	require.False(t, rows[3].PrimaryMessageID.Valid)
}

func TestTranscriptSettlesConcurrentWritersOfOneEntry(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	writer := newTranscriptWriter(t, ti)
	userID := uuid.NewString()
	hooksChat := seedChat(t, ctx, ti, userID, transcriptTestEmail, "hooks chat")
	sessionID := hooksChat.String()
	inferenceChat := seedChat(t, ctx, ti, userID, transcriptTestEmail, "inference chat")

	first := testenv.BeginTx(t, ctx, ti.conn)
	_, err := writer.WriteInTx(ctx, first, []chat.MessageWrite{
		hookWrite(hooksChat, ti.projectID, userID, sessionID, "tool", "result", nil, "toolu_01D"),
	})
	require.NoError(t, err)

	// The second writer resolves nothing (the first key is uncommitted) and
	// then blocks on the unique index until the first transaction ends.
	second := testenv.BeginTx(t, ctx, ti.conn)
	done := make(chan error, 1)
	go func() {
		_, err := writer.WriteInTx(ctx, second, []chat.MessageWrite{
			hookWrite(inferenceChat, ti.projectID, userID, sessionID, "tool", "result", nil, "toolu_01D"),
		})
		done <- err
	}()
	require.NoError(t, first.Commit(ctx))
	require.NoError(t, <-done)
	require.NoError(t, second.Commit(ctx))

	rows := listAllMessages(t, ctx, ti.conn, inferenceChat, ti.projectID)
	require.Len(t, rows, 1)
	require.True(t, rows[0].PrimaryMessageID.Valid, "the writer that lost the race stores a secondary")
	primaries := listAllMessages(t, ctx, ti.conn, hooksChat, ti.projectID)
	require.Len(t, primaries, 1)
	require.Equal(t, primaries[0].ID, rows[0].PrimaryMessageID.UUID)
}

func TestTranscriptDedupsRedeliveryIntoTheSameChat(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	writer := newTranscriptWriter(t, ti)
	userID := uuid.NewString()
	hooksChat := seedChat(t, ctx, ti, userID, transcriptTestEmail, "hooks chat")
	sessionID := hooksChat.String()

	for range 2 {
		_, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{
			hookWrite(hooksChat, ti.projectID, userID, sessionID, "assistant", "", toolCallJSON("toolu_01E"), ""),
		})
		require.NoError(t, err)
	}

	rows := listAllMessages(t, ctx, ti.conn, hooksChat, ti.projectID)
	require.Len(t, rows, 2)
	require.False(t, rows[0].PrimaryMessageID.Valid)
	require.Equal(t, uuid.NullUUID{UUID: rows[0].ID, Valid: true}, rows[1].PrimaryMessageID)
	require.Len(t, meterMessages(t, ti), 1, "a redelivered hook event is metered once")
	require.Equal(t, pgtype.Timestamptz{}.Valid, rows[0].RiskAnalyzedAt.Valid)
	require.True(t, rows[1].RiskAnalyzedAt.Valid)
}
