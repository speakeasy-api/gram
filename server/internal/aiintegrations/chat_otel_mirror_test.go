package aiintegrations

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var testMirrorCreatedAt = time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)

func mirrorTestRow(chatID uuid.UUID, role, content, externalMessageID string) chatrepo.CreateExternalChatMessageParams {
	return chatrepo.CreateExternalChatMessageParams{
		ChatID:            chatID,
		Role:              role,
		ProjectID:         uuid.MustParse("66666666-6666-6666-6666-666666666666"),
		Content:           content,
		ContentRaw:        nil,
		ContentAssetUrl:   pgtype.Text{String: "", Valid: false},
		StorageError:      pgtype.Text{String: "", Valid: false},
		Model:             conv.ToPGText("gpt-5.5"),
		MessageID:         pgtype.Text{String: "", Valid: false},
		ToolCallID:        pgtype.Text{String: "", Valid: false},
		UserID:            pgtype.Text{String: "", Valid: false},
		ExternalUserID:    conv.ToPGText("external-user-id"),
		ExternalMessageID: conv.ToPGText(externalMessageID),
		FinishReason:      pgtype.Text{String: "", Valid: false},
		ToolCalls:         nil,
		PromptTokens:      0,
		CompletionTokens:  0,
		TotalTokens:       0,
		Origin:            pgtype.Text{String: "", Valid: false},
		UserAgent:         pgtype.Text{String: "", Valid: false},
		IpAddress:         pgtype.Text{String: "", Valid: false},
		Source:            conv.ToPGText("chatgpt"),
		ContentHash:       nil,
		Generation:        0,
		CreatedAt:         conv.ToPGTimestamptz(testMirrorCreatedAt),
	}
}

func mirrorRecordAttr(record *otelv1.InboundLogRecord, key string) (string, bool) {
	for _, kv := range record.GetAttributes() {
		if kv.GetKey() == key {
			return kv.GetValue().GetStringValue(), true
		}
	}
	return "", false
}

func TestChatMessageLogRecordMapsRowDeterministically(t *testing.T) {
	t.Parallel()

	cfg := chatgptConversationConfig()
	chatID := uuid.MustParse("77777777-7777-4777-8777-777777777777")
	row := mirrorTestRow(chatID, "user", "what is our refund policy?", "msg_1")

	record := chatMessageLogRecord(cfg, row, "ada@example.com")
	replayed := chatMessageLogRecord(cfg, row, "ada@example.com")

	// The record id is a UUID derived from (chat_id, external_message_id):
	// stable across replays so downstream consumers can dedupe.
	_, err := uuid.Parse(record.GetRecordId())
	require.NoError(t, err)
	require.Equal(t, record.GetRecordId(), replayed.GetRecordId())

	other := chatMessageLogRecord(cfg, mirrorTestRow(chatID, "user", "what is our refund policy?", "msg_2"), "ada@example.com")
	require.NotEqual(t, record.GetRecordId(), other.GetRecordId())

	// Both timestamps come from the row's created_at, never publish time.
	wantNanos := uint64(testMirrorCreatedAt.UnixNano())
	require.Equal(t, wantNanos, record.GetTimeUnixNano())
	require.Equal(t, wantNanos, record.GetObservedTimeUnixNano())

	require.Equal(t, "what is our refund policy?", record.GetBody().GetStringValue())
	require.Equal(t, "gram.compliance.chat_message", record.GetEventName())
	require.Equal(t, otelv1.InboundLogRecord_SEVERITY_NUMBER_INFO, record.GetSeverityNumber())
	require.Equal(t, dialect.ComplianceLogScopeName, record.GetScope().GetName())

	role, ok := mirrorRecordAttr(record, dialect.ComplianceLogRoleAttr)
	require.True(t, ok)
	require.Equal(t, "user", role)
	gotChatID, ok := mirrorRecordAttr(record, dialect.ComplianceLogChatIDAttr)
	require.True(t, ok)
	require.Equal(t, chatID.String(), gotChatID)
	messageID, ok := mirrorRecordAttr(record, dialect.ComplianceLogExternalMessageIDAttr)
	require.True(t, ok)
	require.Equal(t, "msg_1", messageID)
	userID, ok := mirrorRecordAttr(record, dialect.ComplianceLogUserIDAttr)
	require.True(t, ok)
	require.Equal(t, "external-user-id", userID)
	email, ok := mirrorRecordAttr(record, dialect.ComplianceLogUserEmailAttr)
	require.True(t, ok)
	require.Equal(t, "ada@example.com", email)
	model, ok := mirrorRecordAttr(record, "gen_ai.request.model")
	require.True(t, ok)
	require.Equal(t, "gpt-5.5", model)

	require.Len(t, record.GetResource().GetAttributes(), 1)
	require.Equal(t, "service.name", record.GetResource().GetAttributes()[0].GetKey())
	require.Equal(t, "chatgpt", record.GetResource().GetAttributes()[0].GetValue().GetStringValue())

	require.Equal(t, "compliance-import", record.GetProvenance().GetSource())
	require.Equal(t, cfg.OrganizationID, record.GetProvenance().GetOrganizationId())
	require.Equal(t, cfg.ProjectID.String(), record.GetProvenance().GetProjectId())
}

func TestChatMessageLogRecordOmitsUnknownOptionalAttributes(t *testing.T) {
	t.Parallel()

	cfg := chatgptConversationConfig()
	row := mirrorTestRow(uuid.New(), "assistant", "returns are accepted within 30 days", "msg_2")
	row.ExternalUserID = pgtype.Text{String: "", Valid: false}
	row.Model = pgtype.Text{String: "", Valid: false}

	record := chatMessageLogRecord(cfg, row, "")

	_, ok := mirrorRecordAttr(record, dialect.ComplianceLogUserIDAttr)
	require.False(t, ok)
	_, ok = mirrorRecordAttr(record, dialect.ComplianceLogUserEmailAttr)
	require.False(t, ok)
	_, ok = mirrorRecordAttr(record, "gen_ai.request.model")
	require.False(t, ok)

	role, ok := mirrorRecordAttr(record, dialect.ComplianceLogRoleAttr)
	require.True(t, ok)
	require.Equal(t, "assistant", role)
}

func TestChatOTELMirrorPublishesEveryRowWithItsEmail(t *testing.T) {
	t.Parallel()

	capture := &captureOTELLogPublisher{}
	mirror := NewChatOTELMirror(testenv.NewLogger(t), capture)
	cfg := chatgptConversationConfig()
	chatID := uuid.New()

	mirror.PublishMessages(t.Context(), cfg, []chatrepo.CreateExternalChatMessageParams{
		mirrorTestRow(chatID, "user", "hello", "msg_1"),
		mirrorTestRow(chatID, "assistant", "hi there", "msg_2"),
	}, map[string]string{"msg_1": "ada@example.com"})
	mirror.drains.Wait()

	sent := capture.Sent()
	require.Len(t, sent, 2)
	email, ok := mirrorRecordAttr(sent[0], dialect.ComplianceLogUserEmailAttr)
	require.True(t, ok)
	require.Equal(t, "ada@example.com", email)
	_, ok = mirrorRecordAttr(sent[1], dialect.ComplianceLogUserEmailAttr)
	require.False(t, ok, "row without a known email must omit the attribute")

	// Nothing to mirror publishes nothing.
	mirror.PublishMessages(t.Context(), cfg, nil, nil)
	mirror.drains.Wait()
	require.Len(t, capture.Sent(), 2)
}
