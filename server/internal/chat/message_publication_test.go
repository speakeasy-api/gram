package chat_test

import (
	"context"
	"crypto/sha256"
	"io"
	"net/url"
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
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func conversationMessages(t *testing.T, ti *chatTestInstance) []*conversationv1.Message {
	t.Helper()
	rows, err := testrepo.New(ti.conn).ListPublishOutboxRows(t.Context())
	require.NoError(t, err)
	var messages []*conversationv1.Message
	for _, row := range rows {
		if row.Topic != string(proto.MessageName(&conversationv1.Message{})) {
			continue
		}
		message := &conversationv1.Message{}
		require.NoError(t, proto.Unmarshal(row.Message, message))
		require.Equal(t, ti.orgID, row.OrganizationID)
		messages = append(messages, message)
	}
	return messages
}

func TestConversationPublicationIncludesEmptyMessages(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "empty message publication")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, ti.assets)
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	params := minimalChatMessageParams(chatID, ti.projectID)
	params.Content = ""
	writes := []chat.MessageWrite{{Params: params}}
	_, err := writer.Write(ctx, ti.projectID, writes)
	require.NoError(t, err)
	messages := conversationMessages(t, ti)
	require.Len(t, messages, 1)
	require.Equal(t, writes[0].Params.ID.String(), messages[0].GetId())
	require.Equal(t, ti.projectID.String(), messages[0].GetProjectId())
	require.Equal(t, chatID.String(), messages[0].GetConversationId())
	require.Equal(t, conversationv1.Message_ROLE_USER, messages[0].GetRole())
	require.True(t, messages[0].HasBody())
	require.Empty(t, messages[0].GetBody().GetParts())
	require.Empty(t, meterMessages(t, ti))
}

func TestConversationPublicationSpillsLargeBody(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "large message publication")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, ti.assets)
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	params := minimalChatMessageParams(chatID, ti.projectID)
	params.Content = strings.Repeat("large message ", 90000)
	_, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{{Params: params}})
	require.NoError(t, err)
	messages := conversationMessages(t, ti)
	require.Len(t, messages, 1)
	require.False(t, messages[0].HasBody())
	ref := messages[0].GetBodyReference()
	require.NotNil(t, ref)
	uri, err := url.Parse(ref.GetUri())
	require.NoError(t, err)
	reader, err := ti.assets.Read(ctx, uri)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	sum := sha256.Sum256(data)
	require.Equal(t, sum[:], ref.GetSha256())
	require.Equal(t, uint64(len(data)), ref.GetSizeBytes())
	body := &conversationv1.Message_Body{}
	require.NoError(t, proto.Unmarshal(data, body))
	require.Equal(t, params.Content, body.GetParts()[0].GetText())
}

func TestConversationPublicationFailureRollsBackMessageAndMeter(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "publication rollback")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, nil)
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	params := minimalChatMessageParams(chatID, ti.projectID)
	params.Content = strings.Repeat("large message ", 90000)
	_, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{{Params: params}})
	require.ErrorContains(t, err, "publication asset storage unavailable")
	require.Empty(t, listAllMessages(t, ctx, ti.conn, chatID, ti.projectID))
	require.Empty(t, conversationMessages(t, ti))
	require.Empty(t, meterMessages(t, ti))
}

func TestConversationOutboxFailureRollsBackEmptyMessage(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "outbox rollback")
	require.NoError(t, testrepo.New(ti.conn).RejectPublishOutboxWritesFixture(ctx))
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, ti.assets)
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	params := minimalChatMessageParams(chatID, ti.projectID)
	params.Content = ""
	_, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{{Params: params}})
	require.ErrorContains(t, err, "enqueue conversation messages")
	require.Empty(t, listAllMessages(t, ctx, ti.conn, chatID, ti.projectID))
	require.Empty(t, conversationMessages(t, ti))
}

func TestConversationPublicationPreservesImportedPartsAndRawContent(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "imported parts")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, ti.assets)
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	assetURL, err := writer.WriteContentPartAsset(ctx, ti.projectID, chatID, []byte("attached content"))
	require.NoError(t, err)
	params := repo.CreateExternalChatMessageParams{}
	params.ID = uuid.New()
	params.ChatID = chatID
	params.ProjectID = ti.projectID
	params.Role = "user"
	params.Content = "plain text"
	params.ContentRaw = []byte(`[{"type":"text","text":"plain text"}]`)
	params.ExternalMessageID = conv.ToPGText("import-with-parts")
	part := repo.CreateChatContentPartParams{}
	part.ChatID = chatID
	part.ProjectID = ti.projectID
	part.ParentChatMessageID = uuid.NullUUID{UUID: params.ID, Valid: true}
	part.ContentAssetUrl = assetURL
	part.Kind = "text"
	part.CreatedAt = conv.ToPGTimestamptz(time.Now())
	writes := []chat.ExternalMessageWrite{{Params: params}}
	attached := map[uuid.UUID][]repo.CreateChatContentPartParams{params.ID: {part}}
	_, err = writer.WriteExternalWithContentParts(ctx, ti.projectID, writes, attached)
	require.NoError(t, err)
	_, err = writer.WriteExternalWithContentParts(ctx, ti.projectID, writes, attached)
	require.NoError(t, err)
	messages := conversationMessages(t, ti)
	require.Len(t, messages, 1)
	body := messages[0].GetBody()
	require.JSONEq(t, string(params.ContentRaw), string(body.GetSourceContentJson()))
	require.Len(t, body.GetParts(), 2)
	require.Equal(t, params.Content, body.GetParts()[0].GetText())
	require.Equal(t, assetURL, body.GetParts()[1].GetContentReference().GetUri())
}
