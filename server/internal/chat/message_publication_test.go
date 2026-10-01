package chat_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	conversationv1 "github.com/speakeasy-api/gram/infra/gen/gram/conversation/v1"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type publicationAssetGuard struct {
	assets.BlobStore
	pool    *pgxpool.Pool
	uploads int
}

func (a *publicationAssetGuard) Write(ctx context.Context, path, media string, size int64) (io.WriteCloser, *url.URL, error) {
	// With a one-connection pool, an open writer transaction would prevent
	// acquisition. Fail promptly rather than hanging a regression indefinitely.
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := a.pool.Acquire(checkCtx)
	if err != nil {
		return nil, nil, fmt.Errorf("asset upload started while database connection was held: %w", err)
	}
	conn.Release()
	a.uploads++
	w, u, err := a.BlobStore.Write(ctx, path, media, size)
	if err != nil {
		return nil, nil, fmt.Errorf("write guarded publication asset: %w", err)
	}
	return w, u, nil
}

func TestPublicationUploadsBeforeWriterTransactions(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"write", "turn", "external", "caller transaction", "correlated promotion"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ti := newTestChatService(t)
			ctx := initSessionCtx(t, ti)
			chatID := seedChat(t, ctx, ti, "u", "", "pretransaction publication")
			cfg := ti.conn.Config()
			cfg.MaxConns = 1
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			require.NoError(t, err)
			t.Cleanup(pool.Close)
			guard := &publicationAssetGuard{BlobStore: ti.assets, pool: pool}
			writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), pool, guard)
			t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
			params := minimalChatMessageParams(chatID, ti.projectID)
			params.Content = "short display text"
			// Publish the original JSON spelling, independent of jsonb's
			// whitespace and numeric normalization in the persisted row.
			params.ContentRaw = []byte(`{"n":1e2,"payload":"` + strings.Repeat("x", 8*1024*1024) + `"}`)
			writes := []chat.MessageWrite{{Params: params}}
			switch mode {
			case "write":
				_, err = writer.Write(ctx, ti.projectID, writes)
			case "turn":
				err = writer.WriteTurn(ctx, ti.projectID, nil, writes)
			case "external":
				_, err = writer.WriteExternal(ctx, ti.projectID, []chat.ExternalMessageWrite{{Params: repo.CreateExternalChatMessageParams{ID: params.ID, ChatID: chatID, ProjectID: ti.projectID, Role: params.Role, Content: params.Content, ContentRaw: params.ContentRaw, ExternalMessageID: conv.ToPGText("external-large")}}})
			case "caller transaction":
				prepared, prepErr := writer.PreparePublications(ctx, ti.projectID, writes)
				require.NoError(t, prepErr)
				tx := testenv.BeginTx(t, ctx, pool)
				_, err = writer.WriteInTx(ctx, tx, writes, prepared)
				require.NoError(t, err)
				err = tx.Commit(ctx)
			case "correlated promotion":
				writes[0].Params.Source = conv.ToPGText("litellm")
				_, err = writer.WriteCorrelated(ctx, ti.projectID, writes[0], "correlated-large")
				require.NoError(t, err)
				writes[0].Params.ID = uuid.New()
				writes[0].Params.Content = "different incoming body"
				writes[0].Params.ContentRaw = nil
				writes[0].Params.Source = conv.ToPGText("opencode")
				_, err = writer.WriteCorrelated(ctx, ti.projectID, writes[0], "correlated-large")
			}
			require.NoError(t, err)
			messages := conversationMessages(t, ti)
			require.Len(t, messages, 1)
			require.Equal(t, 1, guard.uploads)
			for _, msg := range messages {
				require.True(t, msg.HasBodyReference())
				u, err := url.Parse(msg.GetBodyReference().GetUri())
				require.NoError(t, err)
				r, err := ti.assets.Read(ctx, u)
				require.NoError(t, err)
				data, err := io.ReadAll(r)
				require.NoError(t, err)
				require.NoError(t, r.Close())
				body := &conversationv1.Message_Body{}
				require.NoError(t, proto.Unmarshal(data, body))
				require.Equal(t, params.Content, body.GetParts()[0].GetText())
				require.Equal(t, params.ContentRaw, body.GetSourceContentJson())
			}
		})
	}
}

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

func TestPublicationPreparationFailureOnlyRejectsInsertions(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"external", "correlated"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ti := newTestChatService(t)
			ctx := initSessionCtx(t, ti)
			chatID := seedChat(t, ctx, ti, "u", "", "preparation failure")
			// No asset store: oversized preparations fail deterministically.
			writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, nil)
			t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
			params := minimalChatMessageParams(chatID, ti.projectID)
			params.Source = conv.ToPGText("litellm")
			write := func(externalID string) (int64, error) {
				if mode == "correlated" {
					return writer.WriteCorrelated(ctx, ti.projectID, chat.MessageWrite{Params: params}, externalID)
				}
				return writer.WriteExternal(ctx, ti.projectID, []chat.ExternalMessageWrite{{Params: repo.CreateExternalChatMessageParams{
					ID: params.ID, ChatID: chatID, ProjectID: ti.projectID, Role: params.Role, Content: params.Content,
					ContentRaw: params.ContentRaw, ExternalMessageID: conv.ToPGText(externalID), Source: params.Source,
				}}})
			}
			_, err := write("existing")
			require.NoError(t, err)
			params.ID = uuid.New()
			params.Source = conv.ToPGText("opencode")
			params.ContentRaw = []byte(`{"payload":"` + strings.Repeat("x", 8*1024*1024) + `"}`)
			n, err := write("existing")
			require.NoError(t, err)
			if mode == "correlated" {
				require.EqualValues(t, 1, n)
			} else {
				require.Zero(t, n)
			}
			_, err = write("existing")
			require.NoError(t, err)
			_, err = write("new")
			require.Error(t, err)
			require.Len(t, conversationMessages(t, ti), 1)
			params.ContentRaw = nil
			n, err = write("new")
			require.NoError(t, err)
			require.EqualValues(t, 1, n, "failed preparation must roll back insertion")
		})
	}
}

func TestPublicationPreservesObjectToolArguments(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "object arguments")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, nil)
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	_, err := writer.WriteExternal(ctx, ti.projectID, []chat.ExternalMessageWrite{{Params: repo.CreateExternalChatMessageParams{
		ID: uuid.New(), ChatID: chatID, ProjectID: ti.projectID, Role: "assistant", ExternalMessageID: conv.ToPGText("object-args"),
		ToolCalls: []byte(`[{"id":"call-1","function":{"name":"lookup","arguments":{"query":"example","limit":2}}}]`),
	}}})
	require.NoError(t, err)
	messages := conversationMessages(t, ti)
	require.Len(t, messages, 1)
	require.JSONEq(t, `{"query":"example","limit":2}`, messages[0].GetBody().GetParts()[0].GetToolCall().GetArgumentsJson())
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
	require.False(t, messages[0].GetProvenance().HasBillingUserId())
}

func TestWriteInTxRejectsUnpreparedBodyWithoutUploading(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "stale publication preparation")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, nil)
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	writes := []chat.MessageWrite{{Params: minimalChatMessageParams(chatID, ti.projectID)}}
	prepared, err := writer.PreparePublications(ctx, ti.projectID, writes)
	require.NoError(t, err)
	writes[0].Params.ContentRaw = []byte(`{"payload":"` + strings.Repeat("x", 8*1024*1024) + `"}`)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, err = writer.WriteInTx(ctx, tx, writes, prepared)
	// The nil asset backend must not be consulted while the transaction is open.
	require.ErrorContains(t, err, "body changed after preparation")
	require.NoError(t, tx.Rollback(ctx))
	require.Empty(t, listAllMessages(t, ctx, ti.conn, chatID, ti.projectID))
	require.Empty(t, conversationMessages(t, ti))
	require.Empty(t, meterMessages(t, ti))
}

func TestPreparedInlinePublicationOwnsOriginalJSON(t *testing.T) {
	t.Parallel()
	for _, mutate := range []bool{false, true} {
		t.Run(fmt.Sprintf("mutate=%t", mutate), func(t *testing.T) {
			t.Parallel()
			ti := newTestChatService(t)
			ctx := initSessionCtx(t, ti)
			chatID := seedChat(t, ctx, ti, "u", "", "prepared inline publication")
			writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, nil)
			t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
			params := minimalChatMessageParams(chatID, ti.projectID)
			params.ContentRaw = []byte(`{"n":1e2}`)
			writes := []chat.MessageWrite{{Params: params}}
			prepared, err := writer.PreparePublications(ctx, ti.projectID, writes)
			require.NoError(t, err)
			if mutate {
				params.ContentRaw[5] = '2'
			}
			tx := testenv.BeginTx(t, ctx, ti.conn)
			_, err = writer.WriteInTx(ctx, tx, writes, prepared)
			if mutate {
				require.ErrorContains(t, err, "body changed after preparation")
				require.NoError(t, tx.Rollback(ctx))
				require.Empty(t, conversationMessages(t, ti))
				return
			}
			require.NoError(t, err)
			require.NoError(t, tx.Commit(ctx))
			messages := conversationMessages(t, ti)
			require.Len(t, messages, 1)
			require.Equal(t, `{"n":1e2}`, string(messages[0].GetBody().GetSourceContentJson()))
		})
	}
}

func TestConversationPublicationSeparatesActorAndBillingUser(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "identity publication")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, ti.assets)
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	params := minimalChatMessageParams(chatID, ti.projectID)
	params.UserID = conv.ToPGText("message-user")
	params.ExternalUserID = conv.ToPGText("external-user")
	_, err := writer.Write(ctx, ti.projectID, []chat.MessageWrite{{Params: params, BillingUserID: "billing-user", UserEmail: "actor@example.test"}})
	require.NoError(t, err)
	messages := conversationMessages(t, ti)
	require.Len(t, messages, 1)
	p := messages[0].GetProvenance()
	require.Equal(t, "billing-user", p.GetBillingUserId())
	require.Equal(t, "message-user", p.GetUserId())
	require.Equal(t, "external-user", p.GetExternalUserId())
	require.Equal(t, "actor@example.test", p.GetUserEmail())
}

func TestConversationPublicationSupportsWrappedToolCalls(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "wrapped tool calls")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, ti.assets)
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	params := minimalChatMessageParams(chatID, ti.projectID)
	params.Content = ""
	params.Role = "assistant"
	encoded, err := json.Marshal(`[{"id":"call-1","function":{"name":"lookup","arguments":"{\"query\":\"example\"}"}}]`)
	require.NoError(t, err)
	params.ToolCalls = encoded
	_, err = writer.Write(ctx, ti.projectID, []chat.MessageWrite{{Params: params}})
	require.NoError(t, err)
	require.Len(t, listAllMessages(t, ctx, ti.conn, chatID, ti.projectID), 1)
	messages := conversationMessages(t, ti)
	require.Len(t, messages, 1)
	parts := messages[0].GetBody().GetParts()
	require.Len(t, parts, 1)
	require.Equal(t, "call-1", parts[0].GetToolCall().GetId())
	require.Equal(t, "lookup", parts[0].GetToolCall().GetName())
	require.JSONEq(t, `{"query":"example"}`, parts[0].GetToolCall().GetArgumentsJson())
}

func TestConversationPublicationSpillsLargeBody(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "u", "", "large message publication")
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, ti.assets)
	t.Cleanup(func() { _ = shutdown(context.WithoutCancel(t.Context())) })
	params := minimalChatMessageParams(chatID, ti.projectID)
	params.Content = strings.Repeat("large message ", 600000)
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
	params.Content = strings.Repeat("large message ", 600000)
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
	part.ExternalID = conv.ToPGText("source-file-1")
	part.Metadata = []byte(`{"display_path":"notes/example.txt"}`)
	part.CreatedAt = conv.ToPGTimestamptz(time.Now())
	writes := []chat.ExternalMessageWrite{{Params: params, BillingUserID: "import-billing-user"}}
	attached := map[uuid.UUID][]repo.CreateChatContentPartParams{params.ID: {part}}
	_, err = writer.WriteExternalWithContentParts(ctx, ti.projectID, writes, attached)
	require.NoError(t, err)
	_, err = writer.WriteExternalWithContentParts(ctx, ti.projectID, writes, attached)
	require.NoError(t, err)
	messages := conversationMessages(t, ti)
	require.Len(t, messages, 1)
	body := messages[0].GetBody()
	require.Equal(t, "import-billing-user", messages[0].GetProvenance().GetBillingUserId())
	require.JSONEq(t, string(params.ContentRaw), string(body.GetSourceContentJson()))
	require.Len(t, body.GetParts(), 2)
	require.Equal(t, params.Content, body.GetParts()[0].GetText())
	require.Equal(t, assetURL, body.GetParts()[1].GetContentReference().GetUri())
	require.Equal(t, "source-file-1", body.GetParts()[1].GetContentReference().GetExternalId())
	require.Equal(t, "notes/example.txt", body.GetParts()[1].GetContentReference().GetFilename())
}
