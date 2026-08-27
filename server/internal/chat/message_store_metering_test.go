package chat_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/stokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func meterMessages(t *testing.T, ti *chatTestInstance) []*meteringv1.MeterReading {
	t.Helper()
	rows, err := testrepo.New(ti.conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	messages := make([]*meteringv1.MeterReading, 0, len(rows))
	for _, row := range rows {
		if row.Topic != string(proto.MessageName(&meteringv1.MeterReading{})) {
			continue
		}
		message := &meteringv1.MeterReading{}
		require.NoError(t, proto.Unmarshal(row.Message, message))
		messages = append(messages, message)
	}
	return messages
}

func minimalChatMessageParams(chatID uuid.UUID, projectID uuid.UUID) repo.CreateChatMessageParams {
	param := repo.CreateChatMessageParams{}
	param.ID = uuid.Nil
	param.ChatID = chatID
	param.ProjectID = projectID
	param.Role = "user"
	param.Content = "cross-project message"
	return param
}

func TestChatMessageWriterMetersStoredTextAndToolCalls(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "metered native message")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })

	toolCalls := []byte(`[{"function":{"name":"lookup","arguments":"{\"city\":\"Paris\"}"}}]`)
	params := []repo.CreateChatMessageParams{{
		ID:               uuid.Nil,
		ChatID:           chatID,
		Role:             "assistant",
		ProjectID:        ti.projectID,
		Content:          "Plan a route",
		ContentRaw:       nil,
		ContentAssetUrl:  pgtype.Text{},
		StorageError:     pgtype.Text{},
		Model:            pgtype.Text{},
		MessageID:        pgtype.Text{},
		ToolCallID:       pgtype.Text{},
		UserID:           pgtype.Text{},
		ExternalUserID:   pgtype.Text{},
		FinishReason:     pgtype.Text{},
		ToolCalls:        toolCalls,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		Origin:           pgtype.Text{},
		UserAgent:        pgtype.Text{},
		IpAddress:        pgtype.Text{},
		Source:           conv.ToPGText("codex"),
		ContentHash:      nil,
		Generation:       0,
		Replayed:         false,
		CreatedAt:        pgtype.Timestamptz{},
	}}
	written, err := writer.Write(ctx, ti.projectID, params)
	require.NoError(t, err)
	require.Equal(t, int64(1), written)
	require.NotEqual(t, uuid.Nil, params[0].ID)

	expected, err := stokens.NewCodec().Count(ctx, "Plan a route", "lookup", `{"city":"Paris"}`)
	require.NoError(t, err)
	messages := meterMessages(t, ti)
	require.Len(t, messages, 1)
	require.Equal(t, string(metering.MeterAgentSessionStorage), messages[0].GetMeterId())
	require.Equal(t, "chat_message:"+params[0].ID.String(), messages[0].GetOperationId())
	require.Equal(t, int64(expected), messages[0].GetValue())
	require.NotContains(t, messages[0].GetAttributes(), "codec")
	require.Equal(t, uint32(1), messages[0].GetMeterVersion())
	require.Equal(t, meteringv1.MeterReading_KIND_USAGE, messages[0].GetKind())
	require.Equal(t, string(metering.MeasurementTiktokenO200kBase), messages[0].GetMeasurementMethod())
	require.Equal(t, "chat_message_writer", messages[0].GetSource())
	_, err = time.Parse(time.RFC3339Nano, messages[0].GetProducedAt())
	require.NoError(t, err)
}

func TestChatMessageWriterMetersExternalMessageOnceAtStorageTime(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "metered external message")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })

	historical := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
	param := repo.CreateExternalChatMessageParams{
		ID:                uuid.Nil,
		ChatID:            chatID,
		Role:              "user",
		ProjectID:         ti.projectID,
		Content:           "Imported transcript",
		ContentRaw:        nil,
		ContentAssetUrl:   pgtype.Text{},
		StorageError:      pgtype.Text{},
		Model:             pgtype.Text{},
		MessageID:         pgtype.Text{},
		ToolCallID:        pgtype.Text{},
		UserID:            pgtype.Text{},
		ExternalUserID:    pgtype.Text{},
		ExternalMessageID: conv.ToPGText("external-message-1"),
		FinishReason:      pgtype.Text{},
		ToolCalls:         nil,
		PromptTokens:      0,
		CompletionTokens:  0,
		TotalTokens:       0,
		Origin:            pgtype.Text{},
		UserAgent:         pgtype.Text{},
		IpAddress:         pgtype.Text{},
		Source:            conv.ToPGText("external"),
		ContentHash:       nil,
		Generation:        0,
		CreatedAt:         conv.ToPGTimestamptz(historical),
	}
	before := time.Now().UTC()
	written, err := writer.WriteExternal(ctx, ti.projectID, []repo.CreateExternalChatMessageParams{param})
	require.NoError(t, err)
	require.Equal(t, int64(1), written)
	after := time.Now().UTC()

	messages := meterMessages(t, ti)
	require.Len(t, messages, 1)
	message := messages[0]
	require.Equal(t, string(metering.MeterAgentSessionStorage), message.GetMeterId())
	require.Equal(t, uint32(1), message.GetMeterVersion())
	require.Equal(t, meteringv1.MeterReading_KIND_USAGE, message.GetKind())
	require.Equal(t, string(metering.MeasurementTiktokenO200kBase), message.GetMeasurementMethod())
	require.Equal(t, "chat_message_writer", message.GetSource())
	occurredAt, err := time.Parse(time.RFC3339Nano, message.GetOccurredAt())
	require.NoError(t, err)
	require.False(t, occurredAt.Before(before))
	require.False(t, occurredAt.After(after))
	require.False(t, occurredAt.Equal(historical))

	written, err = writer.WriteExternal(ctx, ti.projectID, []repo.CreateExternalChatMessageParams{param})
	require.NoError(t, err)
	require.Zero(t, written)
	require.Len(t, meterMessages(t, ti), 1)
}

func TestChatMessageWriterRejectsExternalMessageForAnotherProject(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "external project mismatch")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })

	param := repo.CreateExternalChatMessageParams{}
	param.ChatID = chatID
	param.ProjectID = uuid.New()
	param.Role = "user"
	param.Content = "wrong project"
	param.ExternalMessageID = conv.ToPGText("external-project-mismatch")

	written, err := writer.WriteExternal(ctx, ti.projectID, []repo.CreateExternalChatMessageParams{param})

	require.ErrorContains(t, err, "project id does not match")
	require.Zero(t, written)
	require.Empty(t, meterMessages(t, ti))
}

func TestChatMessageWriterRejectsChatOwnedByAnotherProject(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	otherProject := createProjectInSameOrg(t, ti)
	foreignChat := seedChatInProject(t, ti, otherProject, "foreign chat")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })

	written, err := writer.Write(ctx, ti.projectID, []repo.CreateChatMessageParams{
		minimalChatMessageParams(foreignChat, ti.projectID),
	})

	require.ErrorContains(t, err, "chat does not belong to project")
	require.Zero(t, written)
	require.Empty(t, meterMessages(t, ti))
}

func TestChatMessageWriterRejectsCorrelatedChatOwnedByAnotherProject(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	otherProject := createProjectInSameOrg(t, ti)
	foreignChat := seedChatInProject(t, ti, otherProject, "foreign correlated chat")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })

	written, err := writer.WriteCorrelated(
		ctx,
		ti.projectID,
		minimalChatMessageParams(foreignChat, ti.projectID),
		"cross-project-correlation",
	)

	require.ErrorContains(t, err, "chat does not belong to project")
	require.Zero(t, written)
	require.Empty(t, meterMessages(t, ti))
}

func TestChatMessageWriterRejectsExternalChatOwnedByAnotherProject(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	otherProject := createProjectInSameOrg(t, ti)
	foreignChat := seedChatInProject(t, ti, otherProject, "foreign external chat")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })

	param := repo.CreateExternalChatMessageParams{}
	param.ChatID = foreignChat
	param.ProjectID = ti.projectID
	param.Role = "user"
	param.Content = "cross-project external message"
	param.ExternalMessageID = conv.ToPGText("cross-project-external")
	written, err := writer.WriteExternal(ctx, ti.projectID, []repo.CreateExternalChatMessageParams{param})

	require.ErrorContains(t, err, "chat does not belong to project")
	require.Zero(t, written)
	require.Empty(t, meterMessages(t, ti))
}

func TestChatMessageWriterDoesNotMeterCorrelatedPromotion(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "metered correlated message")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })

	base := repo.CreateChatMessageParams{
		ID:               uuid.Nil,
		ChatID:           chatID,
		Role:             "user",
		ProjectID:        ti.projectID,
		Content:          "Correlated prompt",
		ContentRaw:       nil,
		ContentAssetUrl:  pgtype.Text{},
		StorageError:     pgtype.Text{},
		Model:            pgtype.Text{},
		MessageID:        conv.ToPGText("agent-prompt:v1:meter"),
		ToolCallID:       pgtype.Text{},
		UserID:           pgtype.Text{},
		ExternalUserID:   pgtype.Text{},
		FinishReason:     pgtype.Text{},
		ToolCalls:        nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		Origin:           pgtype.Text{},
		UserAgent:        pgtype.Text{},
		IpAddress:        pgtype.Text{},
		Source:           conv.ToPGText("litellm"),
		ContentHash:      nil,
		Generation:       0,
		Replayed:         false,
		CreatedAt:        pgtype.Timestamptz{},
	}
	written, err := writer.WriteCorrelated(ctx, ti.projectID, base, base.MessageID.String)
	require.NoError(t, err)
	require.Equal(t, int64(1), written)
	require.Len(t, meterMessages(t, ti), 1)

	promoted := base
	promoted.ID = uuid.Nil
	promoted.Source = conv.ToPGText("codex")
	written, err = writer.WriteCorrelated(ctx, ti.projectID, promoted, promoted.MessageID.String)
	require.NoError(t, err)
	require.Equal(t, int64(1), written)
	require.Len(t, meterMessages(t, ti), 1)
}

func TestChatMessageWriterWriteInTxRollsBackMessageAndReading(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "metering rollback")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })

	params := []repo.CreateChatMessageParams{{
		ID:               uuid.Nil,
		ChatID:           chatID,
		Role:             "user",
		ProjectID:        ti.projectID,
		Content:          "Rollback this message",
		ContentRaw:       nil,
		ContentAssetUrl:  pgtype.Text{},
		StorageError:     pgtype.Text{},
		Model:            pgtype.Text{},
		MessageID:        pgtype.Text{},
		ToolCallID:       pgtype.Text{},
		UserID:           pgtype.Text{},
		ExternalUserID:   pgtype.Text{},
		FinishReason:     pgtype.Text{},
		ToolCalls:        nil,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
		Origin:           pgtype.Text{},
		UserAgent:        pgtype.Text{},
		IpAddress:        pgtype.Text{},
		Source:           pgtype.Text{},
		ContentHash:      nil,
		Generation:       0,
		Replayed:         false,
		CreatedAt:        pgtype.Timestamptz{},
	}}
	tx, err := ti.conn.Begin(ctx) //nolint:glint // transaction contains only package APIs and SQLc-generated queries
	require.NoError(t, err)
	_, err = writer.WriteInTx(ctx, tx, params)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))

	messages, err := repo.New(ti.conn).ListChatMessages(ctx, repo.ListChatMessagesParams{ChatID: chatID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.Empty(t, messages)
	require.Empty(t, meterMessages(t, ti))
}
