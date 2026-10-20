package chat_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func conversationMessages(t *testing.T, ti *chatTestInstance) []*conversationv1.MessageEvent {
	t.Helper()
	rows, err := testrepo.New(ti.conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	var events []*conversationv1.MessageEvent
	for _, row := range rows {
		require.NotEqual(t, "gram.conversation.v1.Message", row.Topic, "must not publish content snapshots")
		if row.Topic != string(proto.MessageName(&conversationv1.MessageEvent{})) {
			continue
		}
		event := &conversationv1.MessageEvent{}
		require.NoError(t, proto.Unmarshal(row.Message, event))
		require.Equal(t, ti.orgID, row.OrganizationID)
		require.Equal(t, row.PublicID.String(), event.GetId())
		require.Equal(t, conversationv1.MessageEvent_TYPE_CREATED, event.GetType())
		require.NotEqual(t, event.GetMessageId(), event.GetId())
		events = append(events, event)
	}
	return events
}

func TestConversationEventsDoNotUploadLargeBodies(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"write", "turn", "external", "caller transaction", "correlated"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ti := newTestChatService(t)
			ctx := initSessionCtx(t, ti)
			chatID := seedChat(t, ctx, ti, "u", "", "large reference event")
			// No asset store: publication must succeed even above the old spill limit.
			writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, nil)
			t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
			params := minimalChatMessageParams(chatID, ti.projectID)
			params.Content = "short display text"
			params.ContentRaw = []byte(`{"payload":"` + strings.Repeat("x", 9*1024*1024) + `"}`)
			writes := []chat.MessageWrite{{Params: params}}
			var err error
			switch mode {
			case "write":
				_, err = writer.Write(ctx, ti.projectID, writes)
			case "turn":
				err = writer.WriteTurn(ctx, ti.projectID, nil, writes)
			case "external":
				_, err = writer.WriteExternal(ctx, ti.projectID, []chat.ExternalMessageWrite{{Params: repo.CreateExternalChatMessageParams{
					ID: params.ID, ChatID: chatID, ProjectID: ti.projectID, Role: params.Role, Content: params.Content, ContentRaw: params.ContentRaw,
					ExternalMessageID: conv.ToPGText("large-external"),
				}}})
			case "caller transaction":
				tx := testenv.BeginTx(t, ctx, ti.conn)
				_, err = writer.WriteInTx(ctx, tx, writes)
				require.NoError(t, err)
				err = tx.Commit(ctx)
			case "correlated":
				_, err = writer.WriteCorrelated(ctx, ti.projectID, writes[0], "large-correlated")
			}
			require.NoError(t, err)
			events := conversationMessages(t, ti)
			require.Len(t, events, 1)
			require.Less(t, proto.Size(events[0]), 1024)
			stored := listAllMessages(t, ctx, ti.conn, chatID, ti.projectID)
			require.Len(t, stored, 1)
			require.Equal(t, stored[0].ID.String(), events[0].GetMessageId())
			require.JSONEq(t, string(params.ContentRaw), string(stored[0].ContentRaw))
		})
	}
}

func TestConversationEventPreservesIngestionContext(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "event provenance")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, nil)
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	params := minimalChatMessageParams(chatID, ti.projectID)
	params.Content = ""
	params.UserID = conv.ToPGText("message-user")
	params.ExternalUserID = conv.ToPGText("external-user")
	params.Source = conv.ToPGText("opencode")
	params.Replayed = true
	params.CreatedAt = conv.ToPGTimestamptz(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	assistantID := uuid.New()
	_, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{{Params: params, BillingUserID: "billing-user", UserEmail: "actor@example.test",
		AssistantID: assistantID, Provider: "anthropic", HookHostname: "dev-host", AccountType: "team", BillingMode: "flat_rate", WorkloadSource: metering.WorkloadSourceHook,
	}})
	require.NoError(t, err)
	events := conversationMessages(t, ti)
	require.Len(t, events, 1)
	event := events[0]
	require.Equal(t, ti.orgID, event.GetOrganizationId())
	require.Equal(t, ti.projectID.String(), event.GetProjectId())
	require.Equal(t, chatID.String(), event.GetConversationId())
	require.Equal(t, conversationv1.MessageEvent_ROLE_USER, event.GetRole())
	require.Equal(t, "2026-01-02T03:04:05Z", event.GetMessageCreatedAt())
	_, err = time.Parse(time.RFC3339Nano, event.GetOccurredAt())
	require.NoError(t, err)
	ingestion := event.GetIngestion()
	require.Equal(t, "billing-user", ingestion.GetBillingUserId())
	require.Equal(t, "actor@example.test", ingestion.GetObservedUserEmail())
	require.Equal(t, assistantID.String(), ingestion.GetAssistantId())
	require.Equal(t, "anthropic", ingestion.GetProvider())
	require.Equal(t, "opencode", ingestion.GetHookSource())
	require.Equal(t, "dev-host", ingestion.GetHostname())
	require.Equal(t, "team", ingestion.GetAccountType())
	require.Equal(t, "flat_rate", ingestion.GetBillingMode())
	require.True(t, ingestion.GetReplayed())
}

func TestConversationOutboxFailureRollsBackMessageAndMeter(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "outbox rollback")
	require.NoError(t, testrepo.New(ti.conn).RejectPublishOutboxWritesFixture(ctx))
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, nil)
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	params := minimalChatMessageParams(chatID, ti.projectID)
	params.Content = ""
	_, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{{Params: params}})
	require.ErrorContains(t, err, "enqueue conversation events")
	require.Empty(t, listAllMessages(t, ctx, ti.conn, chatID, ti.projectID))
	require.Empty(t, conversationMessages(t, ti))
	require.Empty(t, meterMessages(t, ti))
}

func TestConversationEventReferencesImportedMessageWithAttachments(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "imported parts")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, ti.assets)
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	assetURL, err := writer.WriteContentPartAsset(ctx, ti.projectID, chatID, []byte("attached content"))
	require.NoError(t, err)
	params := repo.CreateExternalChatMessageParams{
		ID: uuid.New(), ChatID: chatID, ProjectID: ti.projectID, Role: "user", Content: "plain text",
		ContentRaw: []byte(`[{"type":"text","text":"plain text"}]`), ExternalMessageID: conv.ToPGText("import-with-parts"),
	}
	part := repo.CreateChatContentPartParams{
		ChatID: chatID, ProjectID: ti.projectID, ParentChatMessageID: uuid.NullUUID{UUID: params.ID, Valid: true},
		ContentAssetUrl: assetURL, Kind: "text", ExternalID: conv.ToPGText("source-file-1"), Metadata: []byte(`{"display_path":"notes/example.txt"}`),
		CreatedAt: conv.ToPGTimestamptz(time.Now()),
	}
	writes := []chat.ExternalMessageWrite{{Params: params, BillingUserID: "import-billing-user"}}
	attached := map[uuid.UUID][]repo.CreateChatContentPartParams{params.ID: {part}}
	_, err = writer.WriteExternalWithContentParts(ctx, ti.projectID, writes, attached)
	require.NoError(t, err)
	n, err := writer.WriteExternalWithContentParts(ctx, ti.projectID, writes, attached)
	require.NoError(t, err)
	require.Zero(t, n)
	events := conversationMessages(t, ti)
	require.Len(t, events, 1)
	require.Equal(t, params.ID.String(), events[0].GetMessageId())
	require.Equal(t, "import-billing-user", events[0].GetIngestion().GetBillingUserId())
	parts, err := repo.New(ti.conn).ListChatContentPartsByChatID(ctx, repo.ListChatContentPartsByChatIDParams{ChatID: chatID, ProjectID: ti.projectID, ParentChatMessageIds: []uuid.UUID{params.ID}})
	require.NoError(t, err)
	require.Len(t, parts, 1)
	require.Equal(t, assetURL, parts[0].ContentAssetUrl)
	require.Equal(t, "source-file-1", parts[0].ExternalID.String)
	require.JSONEq(t, string(part.Metadata), string(parts[0].Metadata))
}
