package chat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/url"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptcrypto"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type sealedAssetsFixture struct {
	shadow *transcriptstore.Store
	assets *transcriptstore.AssetStore
	codec  *transcriptcrypto.Codec
}

func newSealedAssetsFixture(t *testing.T, ti *chatTestInstance) sealedAssetsFixture {
	t.Helper()
	keys := orgencryptiontest.New(t, ti.conn)
	require.NoError(t, keys.Provisioner.EnsureUseCase(t.Context(), ti.orgID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	shadow, err := transcriptstore.New(ti.conn, keys.Service, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	assets, err := transcriptstore.NewAssetStore(ti.assets, keys.Service, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	codec, err := transcriptcrypto.New(keys.Service, ti.orgID)
	require.NoError(t, err)
	return sealedAssetsFixture{shadow: shadow, assets: assets, codec: codec}
}

// sealingWriter is a writer whose project captures shadows and sealed assets.
func (f sealedAssetsFixture) sealingWriter(t *testing.T, ti *chatTestInstance) *chat.ChatMessageWriter {
	t.Helper()
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, ti.assets)
	t.Cleanup(func() { _ = shutdown(t.Context()) })
	return writer.WithTranscriptShadow(f.shadow, ti.projectID).WithSealedAssets(f.assets)
}

func readObject(t *testing.T, ti *chatTestInstance, objectURL *url.URL) []byte {
	t.Helper()
	r, err := ti.assets.Read(t.Context(), objectURL)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	return data
}

// replaceObject swaps an object's bytes, so a test can tell which copy a
// read served.
func replaceObject(t *testing.T, ti *chatTestInstance, objectURL *url.URL, objectPath string, data []byte) {
	t.Helper()
	require.NoError(t, ti.assets.Delete(t.Context(), objectURL))
	w, _, err := ti.assets.Write(t.Context(), objectPath, "application/octet-stream", int64(len(data)))
	require.NoError(t, err)
	_, err = io.Copy(w, bytes.NewReader(data))
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

// captureLargeMessage runs one chat turn whose user message is over the inline
// limit, and returns the stored user row and the object path it was written to.
func captureLargeMessage(t *testing.T, ctx context.Context, ti *chatTestInstance, writer *chat.ChatMessageWriter, body string) (repo.ChatMessage, string) {
	t.Helper()
	strategy := chat.NewChatMessageCaptureStrategy(testenv.NewLogger(t), testenv.NewMeterProvider(t), ti.conn, writer)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	chatID := seedChat(t, ctx, ti, authCtx.UserID, "", "large invented message")
	runTurn(t, t.Context(), strategy, makeRequest(chatID, ti.projectID, ti.orgID, openrouter.CreateMessageUser(body)), makeResponse("noted"))
	for _, m := range listAllMessages(t, t.Context(), ti.conn, chatID, ti.projectID) {
		if m.Role != "user" {
			continue
		}
		require.Empty(t, m.ContentRaw, "a large message keeps its body only in the content asset")
		require.True(t, m.ContentAssetUrl.Valid)
		objectURL, err := url.Parse(m.ContentAssetUrl.String)
		require.NoError(t, err)
		return m, path.Join(ti.projectID.String(), "chats", chatID.String(), path.Base(objectURL.Path))
	}
	require.FailNow(t, "user message not captured")
	return repo.ChatMessage{}, ""
}

func loadedText(t *testing.T, ctx context.Context, ti *chatTestInstance, chatID uuid.UUID, messageID uuid.UUID) string {
	t.Helper()
	result, err := ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	for _, m := range result.Messages {
		if m.ID != messageID.String() {
			continue
		}
		var text string
		require.NoError(t, json.Unmarshal(m.Content, &text))
		return text
	}
	require.FailNow(t, "message not loaded")
	return ""
}

func largeInventedBody() string {
	return strings.Repeat("Invented quarterly harbor ledger: 42 crates of glass marbles. ", 3000)
}

func TestLoadChat_ReadEnabledProjectOpensSealedLargeMessage(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	f := newSealedAssetsFixture(t, ti)
	ctx := authztest.WithAdminGrants(initSessionCtx(t, ti))
	body := largeInventedBody()
	m, objectPath := captureLargeMessage(t, ctx, ti, f.sealingWriter(t, ti), body)

	objectURL, err := url.Parse(m.ContentAssetUrl.String)
	require.NoError(t, err)
	sealed := readObject(t, ti, transcriptstore.SealedAssetURL(objectURL))
	require.NotContains(t, string(sealed), "harbor ledger", "the sealed copy is ciphertext")

	// Swap the plaintext object so the test can tell the copies apart.
	replaceObject(t, ti, objectURL, objectPath, []byte(`"plaintext object was served"`))
	ti.service.WithTranscriptShadowReads(f.shadow, ti.projectID).WithSealedAssetReads(f.assets)

	require.Equal(t, body, loadedText(t, ctx, ti, m.ChatID, m.ID))
}

func TestLoadChat_ReadEnabledProjectServesPlaintextObjectWithoutSealedCopy(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	f := newSealedAssetsFixture(t, ti)
	ctx := authztest.WithAdminGrants(initSessionCtx(t, ti))
	// A writer without sealed assets stands in for objects uploaded before
	// capture began.
	writer, shutdown := chat.NewChatMessageWriter(testenv.NewLogger(t), ti.conn, ti.assets)
	t.Cleanup(func() { _ = shutdown(t.Context()) })
	m, objectPath := captureLargeMessage(t, ctx, ti, writer, largeInventedBody())

	objectURL, err := url.Parse(m.ContentAssetUrl.String)
	require.NoError(t, err)
	exists, err := ti.assets.Exists(t.Context(), transcriptstore.SealedAssetURL(objectURL))
	require.NoError(t, err)
	require.False(t, exists)

	replaceObject(t, ti, objectURL, objectPath, []byte(`"plaintext object was served"`))
	ti.service.WithTranscriptShadowReads(f.shadow, ti.projectID).WithSealedAssetReads(f.assets)

	require.Equal(t, "plaintext object was served", loadedText(t, ctx, ti, m.ChatID, m.ID))
}

func TestLoadChat_ReadEnabledProjectFailsOnASealedCopyFromAnotherChat(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	f := newSealedAssetsFixture(t, ti)
	ctx := authztest.WithAdminGrants(initSessionCtx(t, ti))
	body := largeInventedBody()
	m, objectPath := captureLargeMessage(t, ctx, ti, f.sealingWriter(t, ti), body)
	objectURL, err := url.Parse(m.ContentAssetUrl.String)
	require.NoError(t, err)

	// An authentic sealed copy of the same bytes, bound to another chat.
	jsonBody := readObject(t, ti, objectURL)
	other := transcriptcrypto.AssetIdentity{Kind: transcriptcrypto.AssetMessageContent, ProjectID: ti.projectID, ChatID: uuid.New(), SHA256: transcriptcrypto.ContentHash(jsonBody)}
	forged, err := f.codec.EncryptAsset(t.Context(), other, jsonBody)
	require.NoError(t, err)
	replaceObject(t, ti, transcriptstore.SealedAssetURL(objectURL), transcriptstore.SealedAssetPath(objectPath), forged)
	ti.service.WithTranscriptShadowReads(f.shadow, ti.projectID).WithSealedAssetReads(f.assets)

	_, err = ti.service.LoadChat(ctx, loadPayload(m.ChatID.String()))
	require.ErrorIs(t, err, transcriptstore.ErrStale)
}

func TestLoadChat_ReadEnabledProjectOpensSealedContentParts(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	f := newSealedAssetsFixture(t, ti)
	ctx := authztest.WithAdminGrants(initSessionCtx(t, ti))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	chatID := seedChat(t, ctx, ti, authCtx.UserID, "", "sealed attachments")
	msgID := seedMessageContent(t, ctx, ti, chatID, "please review the attached invented memo")

	attachment := "Invented memo: the lighthouse keeper ordered 300 paper lanterns."
	assetURL, err := f.sealingWriter(t, ti).WriteContentPartAsset(ctx, ti.projectID, chatID, []byte(attachment))
	require.NoError(t, err)
	_, err = repo.New(ti.conn).CreateChatContentPart(ctx, []repo.CreateChatContentPartParams{{
		ChatID:              chatID,
		ProjectID:           ti.projectID,
		Kind:                message.PromptAttachment,
		ContentAssetUrl:     assetURL,
		ExternalID:          pgtype.Text{String: "memo", Valid: true},
		ParentChatMessageID: uuid.NullUUID{UUID: msgID, Valid: true},
		Version:             pgtype.Int4{},
		Source:              pgtype.Text{},
		Metadata:            []byte(`{"display_path":"memo.txt","kind":"file"}`),
		RiskAnalyzedAt:      pgtype.Timestamptz{},
		CreatedAt:           pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}})
	require.NoError(t, err)

	objectURL, err := url.Parse(assetURL)
	require.NoError(t, err)
	require.NotContains(t, string(readObject(t, ti, transcriptstore.SealedAssetURL(objectURL))), "lighthouse")
	objectPath := path.Join(ti.projectID.String(), "chats", chatID.String(), "content-parts", path.Base(objectURL.Path))
	replaceObject(t, ti, objectURL, objectPath, []byte("plaintext object was served"))
	ti.service.WithTranscriptShadowReads(f.shadow, ti.projectID).WithSealedAssetReads(f.assets)

	result, err := ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Len(t, result.ContentParts, 1)
	require.Equal(t, attachment, result.ContentParts[0].Content)

	// A sealed copy that does not open fails the read instead of serving
	// plaintext.
	sealedURL := transcriptstore.SealedAssetURL(objectURL)
	replaceObject(t, ti, sealedURL, transcriptstore.SealedAssetPath(objectPath), []byte("not a sealed object"))
	_, err = ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.Error(t, err)
}

func TestLoadChat_OtherProjectsKeepReadingPlaintextObjects(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	f := newSealedAssetsFixture(t, ti)
	ctx := authztest.WithAdminGrants(initSessionCtx(t, ti))
	m, objectPath := captureLargeMessage(t, ctx, ti, f.sealingWriter(t, ti), largeInventedBody())
	objectURL, err := url.Parse(m.ContentAssetUrl.String)
	require.NoError(t, err)
	replaceObject(t, ti, objectURL, objectPath, []byte(`"plaintext object was served"`))

	// Reads are enabled only for another project.
	ti.service.WithTranscriptShadowReads(f.shadow, uuid.New()).WithSealedAssetReads(f.assets)

	require.Equal(t, "plaintext object was served", loadedText(t, ctx, ti, m.ChatID, m.ID))
}

func TestLoadChat_UnverifiableContentPartFailsWhenACopyExists(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	f := newSealedAssetsFixture(t, ti)
	ctx := authztest.WithAdminGrants(initSessionCtx(t, ti))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	chatID := seedChat(t, ctx, ti, authCtx.UserID, "", "unverifiable attachment")
	msgID := seedMessageContent(t, ctx, ti, chatID, "please review the attached invented memo")

	// An object whose name is not a content hash cannot name its sealed
	// identity.
	attachment := "Invented memo: the ferry timetable lists 14 crossings."
	objectPath := path.Join(ti.projectID.String(), "chats", chatID.String(), "content-parts", "notes.txt")
	w, objectURL, err := ti.assets.Write(ctx, objectPath, "text/plain", int64(len(attachment)))
	require.NoError(t, err)
	_, err = io.Copy(w, strings.NewReader(attachment))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	_, err = repo.New(ti.conn).CreateChatContentPart(ctx, []repo.CreateChatContentPartParams{{
		ChatID:              chatID,
		ProjectID:           ti.projectID,
		Kind:                message.PromptAttachment,
		ContentAssetUrl:     objectURL.String(),
		ExternalID:          pgtype.Text{String: "notes", Valid: true},
		ParentChatMessageID: uuid.NullUUID{UUID: msgID, Valid: true},
		Version:             pgtype.Int4{},
		Source:              pgtype.Text{},
		Metadata:            []byte(`{"display_path":"notes.txt","kind":"file"}`),
		RiskAnalyzedAt:      pgtype.Timestamptz{},
		CreatedAt:           pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
	}})
	require.NoError(t, err)
	ti.service.WithTranscriptShadowReads(f.shadow, ti.projectID).WithSealedAssetReads(f.assets)

	// Without a copy next to it, the part serves plaintext.
	result, err := ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.NoError(t, err)
	require.Len(t, result.ContentParts, 1)
	require.Equal(t, attachment, result.ContentParts[0].Content)

	// With one, the read fails instead of skipping the copy.
	w, _, err = ti.assets.Write(ctx, transcriptstore.SealedAssetPath(objectPath), "application/octet-stream", 6)
	require.NoError(t, err)
	_, err = io.Copy(w, strings.NewReader("sealed"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	_, err = ti.service.LoadChat(ctx, loadPayload(chatID.String()))
	require.ErrorIs(t, err, transcriptstore.ErrStale)
}
