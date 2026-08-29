package metering_test

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
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
	meteringchrepo "github.com/speakeasy-api/gram/server/internal/metering/chrepo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/stokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestChatStorageReadingPipelineToClickHouse(t *testing.T) {
	t.Parallel()
	conn, organizationID := newMeteringPostgres(t)
	ctx := t.Context()
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Metering Pipeline Project",
		Slug:           "metering-pipeline-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	messageUserID := "pipeline-message-user-" + uuid.NewString()
	ownerUserID := "pipeline-owner-user-" + uuid.NewString()
	seedMeteringUser(t, conn, organizationID, messageUserID, "pipeline-message-account@example.test", "Pipeline Message Division", "Pipeline Message Department", true)
	seedMeteringUser(t, conn, organizationID, ownerUserID, "pipeline-owner-account@example.test", "Pipeline Owner Division", "Pipeline Owner Department", true)
	seedMeteringRole(t, conn, organizationID, messageUserID, "zeta-pipeline", false)
	seedMeteringRole(t, conn, organizationID, messageUserID, "alpha-pipeline", true)
	seedMeteringRole(t, conn, organizationID, ownerUserID, "member-owner-pipeline", false)
	seedMeteringRole(t, conn, organizationID, ownerUserID, "admin-owner-pipeline", true)

	chatID := uuid.New()
	_, err = chatrepo.New(conn).UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      project.ID,
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(ownerUserID),
		ExternalUserID: conv.ToPGText("pipeline-owner-provider-id"),
		Title:          conv.ToPGText("Metering pipeline"),
	})
	require.NoError(t, err)

	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), conn, assetstest.NewTestBlobStore(t))
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	writes := []chat.MessageWrite{{
		Params: chatrepo.CreateChatMessageParams{
			ID:               uuid.Nil,
			ChatID:           chatID,
			Role:             "user",
			ProjectID:        project.ID,
			Content:          "Plan a route",
			ContentRaw:       nil,
			ContentAssetUrl:  pgtype.Text{},
			StorageError:     pgtype.Text{},
			Model:            conv.ToPGText("pipeline-model"),
			MessageID:        pgtype.Text{},
			ToolCallID:       pgtype.Text{},
			UserID:           conv.ToPGText(messageUserID),
			ExternalUserID:   conv.ToPGText("pipeline-message-provider-id"),
			FinishReason:     pgtype.Text{},
			ToolCalls:        nil,
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
		UserEmail: "pipeline-observed@example.test",
	}}
	written, err := writer.Write(ctx, project.ID, writes)
	require.NoError(t, err)
	require.Equal(t, int64(1), written)

	storedMessages, err := chatrepo.New(conn).ListChatMessages(ctx, chatrepo.ListChatMessagesParams{
		ChatID:    chatID,
		ProjectID: project.ID,
	})
	require.NoError(t, err)
	require.Len(t, storedMessages, 1)
	storedID := storedMessages[0].ID

	outboxRows, err := testrepo.New(conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Len(t, outboxRows, 1)
	message := &meteringv1.MeterReading{}
	require.NoError(t, proto.Unmarshal(outboxRows[0].Message, message))
	expected, err := stokens.NewCodec().Count(ctx, "Plan a route")
	require.NoError(t, err)
	require.Equal(t, string(metering.MeterAgentSessionStorage), message.GetMeterId())
	require.Equal(t, string(metering.UnitSTokens), message.GetUnit())
	require.Equal(t, map[string]string{
		metering.AttributeChatID:                chatID.String(),
		metering.AttributeModel:                 "pipeline-model",
		metering.AttributeHookSource:            "codex",
		metering.AttributeMessageUserID:         messageUserID,
		metering.AttributeMessageExternalUserID: "pipeline-message-provider-id",
		metering.AttributeMessageUserEmail:      "pipeline-observed@example.test",
	}, message.GetAttributes())
	require.Equal(t, int64(expected), message.GetValue())
	require.Equal(t, uint32(1), message.GetMeterVersion())
	require.Equal(t, meteringv1.MeterReading_KIND_USAGE, message.GetKind())
	require.Equal(t, string(metering.MeasurementTiktokenO200kBase), message.GetMeasurementMethod())
	require.Equal(t, "chat_message_writer", message.GetSource())
	require.Equal(t, "chat_message:"+storedID.String(), message.GetOperationId())
	occurredAt, err := time.Parse(time.RFC3339Nano, message.GetOccurredAt())
	require.NoError(t, err)
	producedAt, err := time.Parse(time.RFC3339Nano, message.GetProducedAt())
	require.NoError(t, err)
	expectedReading, err := metering.NewUsage(metering.UsageInput{
		Meter:       metering.AgentSessionStorage(),
		Scope:       metering.ProjectScope(organizationID, project.ID),
		OperationID: message.GetOperationId(),
		Value:       message.GetValue(),
		OccurredAt:  occurredAt,
		ProducedAt:  producedAt,
		Source:      message.GetSource(),
		Attributes:  message.GetAttributes(),
	})
	require.NoError(t, err)
	require.Equal(t, expectedReading.ID().String(), message.GetId())

	clickhouseConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	chWriter := metering.NewMeterReadingCHWriter(testenv.NewLogger(t), conn, meteringchrepo.New(clickhouseConn), true)
	require.NoError(t, chWriter.HandleBatch(ctx, []*meteringv1.MeterReading{message}, nil))
	require.NoError(t, chWriter.HandleBatch(ctx, []*meteringv1.MeterReading{message}, nil))

	var value int64
	var attributes map[string]string
	require.NoError(t, clickhouseConn.QueryRow(ctx, `
		SELECT value, attributes
		FROM billing_meter_readings FINAL
		WHERE organization_id = ? AND project_id = ? AND meter_id = ? AND id = ?
	`, organizationID, project.ID, string(metering.MeterAgentSessionStorage), expectedReading.ID()).Scan(&value, &attributes))
	require.Equal(t, int64(expected), value)
	require.Equal(t, map[string]string{
		"codec":                                     string(metering.MeasurementTiktokenO200kBase),
		metering.AttributeChatID:                    chatID.String(),
		metering.AttributeModel:                     "pipeline-model",
		metering.AttributeHookSource:                "codex",
		metering.AttributeMessageUserID:             messageUserID,
		metering.AttributeMessageExternalUserID:     "pipeline-message-provider-id",
		metering.AttributeMessageUserEmail:          "pipeline-observed@example.test",
		metering.AttributeMessageUserAccountEmail:   "pipeline-message-account@example.test",
		metering.AttributeMessageUserDivisionName:   "Pipeline Message Division",
		metering.AttributeMessageUserDepartmentName: "Pipeline Message Department",
		metering.AttributeMessageUserDirectoryMatch: "user_id",
		metering.AttributeMessageUserRBACRoles:      `["alpha-pipeline","zeta-pipeline"]`,
		metering.AttributeChatOwnerUserID:           ownerUserID,
		metering.AttributeChatOwnerExternalUserID:   "pipeline-owner-provider-id",
		metering.AttributeChatOwnerUserEmail:        "pipeline-owner-account@example.test",
		metering.AttributeChatOwnerDivisionName:     "Pipeline Owner Division",
		metering.AttributeChatOwnerDepartmentName:   "Pipeline Owner Department",
		metering.AttributeChatOwnerDirectoryMatch:   "user_id",
		metering.AttributeChatOwnerRBACRoles:        `["admin-owner-pipeline","member-owner-pipeline"]`,
	}, attributes)
	for _, genericKey := range []string{"user_id", "user_email", "division_name", "department_name", "roles"} {
		require.NotContains(t, attributes, genericKey)
	}
}
