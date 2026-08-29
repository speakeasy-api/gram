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
	messageUserID := uuid.NewString()
	writes := []chat.MessageWrite{{
		Params: repo.CreateChatMessageParams{
			ID:               uuid.Nil,
			ChatID:           chatID,
			Role:             "assistant",
			ProjectID:        ti.projectID,
			Content:          "Plan a route",
			ContentRaw:       nil,
			ContentAssetUrl:  pgtype.Text{},
			StorageError:     pgtype.Text{},
			Model:            conv.ToPGText("gpt-5"),
			MessageID:        pgtype.Text{},
			ToolCallID:       pgtype.Text{},
			UserID:           conv.ToPGText(messageUserID),
			ExternalUserID:   conv.ToPGText("provider-user-123"),
			FinishReason:     pgtype.Text{},
			ToolCalls:        toolCalls,
			PromptTokens:     0,
			CompletionTokens: 0,
			TotalTokens:      0,
			Origin:           pgtype.Text{},
			UserAgent:        pgtype.Text{},
			IpAddress:        pgtype.Text{},
			Source:           conv.ToPGText("Codex"),
			ContentHash:      nil,
			Generation:       0,
			Replayed:         false,
			CreatedAt:        pgtype.Timestamptz{},
		},
		UserEmail:    "reported@example.test",
		Provider:     "openai",
		HookHostname: "workstation.example.test",
		AccountType:  "team",
		BillingMode:  "metered",
	}}
	written, err := writer.Write(ctx, ti.projectID, writes)
	require.NoError(t, err)
	require.Equal(t, int64(1), written)
	require.NotEqual(t, uuid.Nil, writes[0].Params.ID)

	expected, err := stokens.NewCodec().Count(ctx, "Plan a route", "lookup", `{"city":"Paris"}`)
	require.NoError(t, err)
	messages := meterMessages(t, ti)
	require.Len(t, messages, 1)
	require.Equal(t, string(metering.MeterAgentSessionStorage), messages[0].GetMeterId())
	require.Equal(t, "chat_message:"+writes[0].Params.ID.String(), messages[0].GetOperationId())
	require.Equal(t, int64(expected), messages[0].GetValue())
	require.Equal(t, map[string]string{
		metering.AttributeChatID:                chatID.String(),
		metering.AttributeModel:                 "gpt-5",
		metering.AttributeProvider:              "openai",
		metering.AttributeHookSource:            "codex",
		metering.AttributeHookHostname:          "workstation.example.test",
		metering.AttributeAccountType:           "team",
		metering.AttributeBillingMode:           "metered",
		metering.AttributeMessageUserID:         messageUserID,
		metering.AttributeMessageExternalUserID: "provider-user-123",
		metering.AttributeMessageUserEmail:      "reported@example.test",
	}, messages[0].GetAttributes())
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
	param := chat.ExternalMessageWrite{
		Params: repo.CreateExternalChatMessageParams{
			ID:                uuid.Nil,
			ChatID:            chatID,
			Role:              "user",
			ProjectID:         ti.projectID,
			Content:           "Imported transcript",
			ContentRaw:        nil,
			ContentAssetUrl:   pgtype.Text{},
			StorageError:      pgtype.Text{},
			Model:             conv.ToPGText("imported-model"),
			MessageID:         pgtype.Text{},
			ToolCallID:        pgtype.Text{},
			UserID:            pgtype.Text{},
			ExternalUserID:    conv.ToPGText("opaque-provider-user-456"),
			ExternalMessageID: conv.ToPGText("external-message-1"),
			FinishReason:      pgtype.Text{},
			ToolCalls:         nil,
			PromptTokens:      0,
			CompletionTokens:  0,
			TotalTokens:       0,
			Origin:            pgtype.Text{},
			UserAgent:         pgtype.Text{},
			IpAddress:         pgtype.Text{},
			Source:            conv.ToPGText("ChatGPT"),
			ContentHash:       nil,
			Generation:        0,
			CreatedAt:         conv.ToPGTimestamptz(historical),
		},
		UserEmail:    "imported@example.test",
		Provider:     "openai",
		HookHostname: "",
		AccountType:  "team",
		BillingMode:  "flat_rate",
	}
	before := time.Now().UTC()
	written, err := writer.WriteExternal(ctx, ti.projectID, []chat.ExternalMessageWrite{param})
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
	require.Equal(t, map[string]string{
		metering.AttributeChatID:                chatID.String(),
		metering.AttributeModel:                 "imported-model",
		metering.AttributeProvider:              "openai",
		metering.AttributeHookSource:            "chatgpt",
		metering.AttributeAccountType:           "team",
		metering.AttributeBillingMode:           "flat_rate",
		metering.AttributeMessageExternalUserID: "opaque-provider-user-456",
		metering.AttributeMessageUserEmail:      "imported@example.test",
	}, message.GetAttributes())
	occurredAt, err := time.Parse(time.RFC3339Nano, message.GetOccurredAt())
	require.NoError(t, err)
	require.False(t, occurredAt.Before(before))
	require.False(t, occurredAt.After(after))
	require.False(t, occurredAt.Equal(historical))

	written, err = writer.WriteExternal(ctx, ti.projectID, []chat.ExternalMessageWrite{param})
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

	written, err := writer.WriteExternal(ctx, ti.projectID, []chat.ExternalMessageWrite{{Params: param, UserEmail: ""}})

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

	written, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{{
		Params:    minimalChatMessageParams(foreignChat, ti.projectID),
		UserEmail: "",
	}})

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
		chat.MessageWrite{Params: minimalChatMessageParams(foreignChat, ti.projectID), UserEmail: ""},
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
	written, err := writer.WriteExternal(ctx, ti.projectID, []chat.ExternalMessageWrite{{Params: param, UserEmail: ""}})

	require.ErrorContains(t, err, "chat does not belong to project")
	require.Zero(t, written)
	require.Empty(t, meterMessages(t, ti))
}

func TestChatMessageWriterUpdatesCorrelatedPromotionReading(t *testing.T) {
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
	written, err := writer.WriteCorrelated(ctx, ti.projectID, chat.MessageWrite{
		Params:       base,
		UserEmail:    "proxy-observed@example.test",
		Provider:     "openai",
		HookHostname: "",
		AccountType:  "",
		BillingMode:  "",
	}, base.MessageID.String)
	require.NoError(t, err)
	require.Equal(t, int64(1), written)
	initialReadings := meterMessages(t, ti)
	require.Len(t, initialReadings, 1)

	promoted := base
	promoted.ID = uuid.Nil
	promoted.Content = "Native hook content must not replace the persisted correlated prompt when metering"
	promoted.Source = conv.ToPGText("codex")
	written, err = writer.WriteCorrelated(ctx, ti.projectID, chat.MessageWrite{
		Params:       promoted,
		UserEmail:    "native-observed@example.test",
		Provider:     "openai",
		HookHostname: "workstation.example.test",
		AccountType:  "team",
		BillingMode:  "metered",
	}, promoted.MessageID.String)
	require.NoError(t, err)
	require.Equal(t, int64(1), written)

	expectedValue, err := stokens.NewCodec().Count(ctx, base.Content)
	require.NoError(t, err)
	incomingValue, err := stokens.NewCodec().Count(ctx, promoted.Content)
	require.NoError(t, err)
	require.NotEqual(t, expectedValue, incomingValue)

	readings := meterMessages(t, ti)
	require.Len(t, readings, 2)
	require.Equal(t, initialReadings[0].GetId(), readings[1].GetId())
	require.Equal(t, initialReadings[0].GetOperationId(), readings[1].GetOperationId())
	require.Equal(t, int64(expectedValue), readings[1].GetValue())
	require.Equal(t, map[string]string{
		metering.AttributeChatID:           chatID.String(),
		metering.AttributeProvider:         "openai",
		metering.AttributeHookSource:       "codex",
		metering.AttributeHookHostname:     "workstation.example.test",
		metering.AttributeAccountType:      "team",
		metering.AttributeBillingMode:      "metered",
		metering.AttributeMessageUserEmail: "native-observed@example.test",
	}, readings[1].GetAttributes())
}

func TestChatMessageWriterWriteInTxRollsBackMessageAndReading(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "metering rollback")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })

	writes := []chat.MessageWrite{{
		Params: repo.CreateChatMessageParams{
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
		},
		UserEmail: "",
	}}
	tx, err := ti.conn.Begin(ctx) //nolint:glint // transaction contains only package APIs and SQLc-generated queries
	require.NoError(t, err)
	_, err = writer.WriteInTx(ctx, tx, writes)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))

	messages, err := repo.New(ti.conn).ListChatMessages(ctx, repo.ListChatMessagesParams{ChatID: chatID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.Empty(t, messages)
	require.Empty(t, meterMessages(t, ti))
}
