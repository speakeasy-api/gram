package assets_test

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/assets"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/chat/transcriptstore"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const inventedAttachment = "Invented field notes: the orchard counted 512 blue pears this spring."

func sealedAttachmentStore(t *testing.T, ctx context.Context, ti *testInstance) (*transcriptstore.AssetStore, uuid.UUID) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	keys := orgencryptiontest.New(t, ti.conn)
	require.NoError(t, keys.Provisioner.EnsureUseCase(ctx, authCtx.ActiveOrganizationID, orgencryption.UseCaseChatTranscripts, orgencryption.SystemActor()))
	store, err := transcriptstore.NewAssetStore(ti.storage, keys.Service, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t))
	require.NoError(t, err)
	return store, *authCtx.ProjectID
}

func uploadAttachment(t *testing.T, ctx context.Context, ti *testInstance, content string) (uuid.UUID, *url.URL) {
	t.Helper()
	result, err := ti.service.UploadChatAttachment(ctx, &gen.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "text/plain",
		ContentLength:    int64(len(content)),
	}, io.NopCloser(strings.NewReader(content)))
	require.NoError(t, err)
	assetID := uuid.MustParse(result.Asset.ID)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	row, err := ti.repo.GetChatAttachmentAssetURL(ctx, repo.GetChatAttachmentAssetURLParams{ID: assetID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	objectURL, err := url.Parse(row.Url)
	require.NoError(t, err)
	return assetID, objectURL
}

func readStored(t *testing.T, ctx context.Context, ti *testInstance, objectURL *url.URL) []byte {
	t.Helper()
	r, err := ti.storage.Read(ctx, objectURL)
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	return data
}

// replaceStored swaps an object's bytes. The test blob store writes relative
// object paths, whose first segment parses back as the URL host.
func replaceStored(t *testing.T, ctx context.Context, ti *testInstance, objectURL *url.URL, data []byte) {
	t.Helper()
	require.NoError(t, ti.storage.Delete(ctx, objectURL))
	objectPath := strings.TrimPrefix(objectURL.Host+objectURL.Path, "/")
	w, written, err := ti.storage.Write(ctx, objectPath, "application/octet-stream", int64(len(data)))
	require.NoError(t, err)
	require.Equal(t, objectURL.String(), written.String())
	_, err = io.Copy(w, bytes.NewReader(data))
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

func serveBoth(t *testing.T, ctx context.Context, ti *testInstance, projectID, assetID uuid.UUID) (authenticated string, signed string) {
	t.Helper()
	_, body, err := ti.service.ServeChatAttachment(ctx, &gen.ServeChatAttachmentForm{
		SessionToken: nil, ApikeyToken: nil, ProjectID: projectID.String(), ID: assetID.String(),
	})
	require.NoError(t, err)
	a, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())

	token, _, err := assets.GenerateSignedAssetToken("test-jwt-secret", assetID, projectID, time.Minute)
	require.NoError(t, err)
	_, body, err = ti.service.ServeChatAttachmentSigned(context.WithoutCancel(ctx), &gen.ServeChatAttachmentSignedForm{Token: token})
	require.NoError(t, err)
	s, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	return string(a), string(s)
}

func TestService_SealedChatAttachment_ServedFromEncryptedCopy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	store, projectID := sealedAttachmentStore(t, ctx, ti)
	ti.service.WithSealedChatAttachments(store, projectID)

	assetID, objectURL := uploadAttachment(t, ctx, ti, inventedAttachment)
	sealed := readStored(t, ctx, ti, transcriptstore.SealedAssetURL(objectURL))
	require.NotContains(t, string(sealed), "orchard", "the sealed copy is ciphertext")

	// Swap the plaintext object so the test can tell the copies apart.
	replaceStored(t, ctx, ti, objectURL, []byte("plaintext object was served"))

	authenticated, signed := serveBoth(t, ctx, ti, projectID, assetID)
	require.Equal(t, inventedAttachment, authenticated)
	require.Equal(t, inventedAttachment, signed)
}

func TestService_SealedChatAttachment_PlaintextWithoutEncryptedCopy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	store, projectID := sealedAttachmentStore(t, ctx, ti)

	// Uploaded before capture began: no encrypted copy.
	assetID, objectURL := uploadAttachment(t, ctx, ti, inventedAttachment)
	exists, err := ti.storage.Exists(ctx, transcriptstore.SealedAssetURL(objectURL))
	require.NoError(t, err)
	require.False(t, exists)

	ti.service.WithSealedChatAttachments(store, projectID)
	authenticated, signed := serveBoth(t, ctx, ti, projectID, assetID)
	require.Equal(t, inventedAttachment, authenticated)
	require.Equal(t, inventedAttachment, signed)
}

func TestService_SealedChatAttachment_OtherProjectsServePlaintext(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	store, projectID := sealedAttachmentStore(t, ctx, ti)
	ti.service.WithSealedChatAttachments(store, uuid.New())

	assetID, objectURL := uploadAttachment(t, ctx, ti, inventedAttachment)
	exists, err := ti.storage.Exists(ctx, transcriptstore.SealedAssetURL(objectURL))
	require.NoError(t, err)
	require.False(t, exists, "uploads to other projects are not sealed")

	authenticated, signed := serveBoth(t, ctx, ti, projectID, assetID)
	require.Equal(t, inventedAttachment, authenticated)
	require.Equal(t, inventedAttachment, signed)
}

func TestService_SealedChatAttachment_UnopenableCopyFailsTheDownload(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	store, projectID := sealedAttachmentStore(t, ctx, ti)
	ti.service.WithSealedChatAttachments(store, projectID)

	assetID, objectURL := uploadAttachment(t, ctx, ti, inventedAttachment)
	replaceStored(t, ctx, ti, transcriptstore.SealedAssetURL(objectURL), []byte("not a sealed object"))

	_, _, err := ti.service.ServeChatAttachment(ctx, &gen.ServeChatAttachmentForm{
		SessionToken: nil, ApikeyToken: nil, ProjectID: projectID.String(), ID: assetID.String(),
	})
	require.Error(t, err)

	token, _, err := assets.GenerateSignedAssetToken("test-jwt-secret", assetID, projectID, time.Minute)
	require.NoError(t, err)
	_, _, err = ti.service.ServeChatAttachmentSigned(ctx, &gen.ServeChatAttachmentSignedForm{Token: token})
	require.Error(t, err)
}

func TestService_SealedChatAttachment_DeduplicatedUploadSealsTheReusedAsset(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	store, projectID := sealedAttachmentStore(t, ctx, ti)

	// Uploaded before capture began: no encrypted copy.
	assetID, objectURL := uploadAttachment(t, ctx, ti, inventedAttachment)
	exists, err := ti.storage.Exists(ctx, transcriptstore.SealedAssetURL(objectURL))
	require.NoError(t, err)
	require.False(t, exists)

	// The same bytes uploaded again reuse the asset and seal it.
	ti.service.WithSealedChatAttachments(store, projectID)
	reusedID, _ := uploadAttachment(t, ctx, ti, inventedAttachment)
	require.Equal(t, assetID, reusedID)
	require.NotContains(t, string(readStored(t, ctx, ti, transcriptstore.SealedAssetURL(objectURL))), "orchard")

	replaceStored(t, ctx, ti, objectURL, []byte(strings.Repeat("x", len(inventedAttachment))))
	authenticated, signed := serveBoth(t, ctx, ti, projectID, assetID)
	require.Equal(t, inventedAttachment, authenticated)
	require.Equal(t, inventedAttachment, signed)
}

func TestService_SealedChatAttachment_UnverifiableRowFailsWhenACopyExists(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	store, projectID := sealedAttachmentStore(t, ctx, ti)
	ti.service.WithSealedChatAttachments(store, projectID)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	// A row whose sha256 is not a content hash cannot name its sealed
	// identity.
	objectPath := projectID.String() + "/invented-notes.txt"
	w, objectURL, err := ti.storage.Write(ctx, objectPath, "text/plain", int64(len(inventedAttachment)))
	require.NoError(t, err)
	_, err = io.Copy(w, strings.NewReader(inventedAttachment))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	asset, err := ti.repo.CreateAsset(ctx, repo.CreateAssetParams{
		Name:           "invented-notes.txt",
		Url:            objectURL.String(),
		ProjectID:      projectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Sha256:         "not-a-content-hash",
		Kind:           "chat_attachment",
		ContentType:    "text/plain",
		ContentLength:  int64(len(inventedAttachment)),
	})
	require.NoError(t, err)

	// Without a copy next to it, the download serves plaintext.
	authenticated, _ := serveBoth(t, ctx, ti, projectID, asset.ID)
	require.Equal(t, inventedAttachment, authenticated)

	// With one, it fails instead of skipping the copy.
	w, _, err = ti.storage.Write(ctx, objectPath+transcriptstore.SealedAssetSuffix, "application/octet-stream", 6)
	require.NoError(t, err)
	_, err = io.Copy(w, strings.NewReader("sealed"))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	_, _, err = ti.service.ServeChatAttachment(ctx, &gen.ServeChatAttachmentForm{
		SessionToken: nil, ApikeyToken: nil, ProjectID: projectID.String(), ID: asset.ID.String(),
	})
	require.Error(t, err)
}
