package assets_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/assets"
	srv "github.com/speakeasy-api/gram/server/gen/http/assets/server"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_UploadChatAttachment_Success_AudioMp3(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	content := "fake mp3 audio content"
	sha := sha256.Sum256([]byte(content))
	expectedSha256 := hex.EncodeToString(sha[:])
	contentType := "audio/mpeg"
	contentLength := int64(len(content))

	result, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(content)))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)

	require.NotEqual(t, uuid.Nil.String(), result.Asset.ID)
	require.Equal(t, "chat_attachment", result.Asset.Kind)
	require.Equal(t, contentType, result.Asset.ContentType)
	require.Equal(t, contentLength, result.Asset.ContentLength)
	require.Equal(t, expectedSha256, result.Asset.Sha256)
	require.NotEmpty(t, result.Asset.CreatedAt)
	require.NotEmpty(t, result.Asset.UpdatedAt)

	// Verify the URL is correctly formatted
	expectedURL := fmt.Sprintf("%s?id=%s&project_id=%s", srv.ServeChatAttachmentAssetsPath(), result.Asset.ID, projectID.String())
	require.Equal(t, expectedURL, result.URL)
}

func TestService_UploadChatAttachment_Success_AudioWav(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	content := "fake wav audio content"
	sha := sha256.Sum256([]byte(content))
	expectedSha256 := hex.EncodeToString(sha[:])
	contentType := "audio/wav"
	contentLength := int64(len(content))

	result, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(content)))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)

	require.NotEqual(t, uuid.Nil.String(), result.Asset.ID)
	require.Equal(t, "chat_attachment", result.Asset.Kind)
	require.Equal(t, contentType, result.Asset.ContentType)
	require.Equal(t, contentLength, result.Asset.ContentLength)
	require.Equal(t, expectedSha256, result.Asset.Sha256)
}

func TestService_UploadChatAttachment_Success_ImagePng(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	content := "fake png image content for chat"
	sha := sha256.Sum256([]byte(content))
	expectedSha256 := hex.EncodeToString(sha[:])
	contentType := "image/png"
	contentLength := int64(len(content))

	result, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(content)))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)

	require.NotEqual(t, uuid.Nil.String(), result.Asset.ID)
	require.Equal(t, "chat_attachment", result.Asset.Kind)
	require.Equal(t, contentType, result.Asset.ContentType)
	require.Equal(t, contentLength, result.Asset.ContentLength)
	require.Equal(t, expectedSha256, result.Asset.Sha256)
}

func TestService_UploadChatAttachment_Success_ImageJpeg(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	content := "fake jpeg image content for chat"
	sha := sha256.Sum256([]byte(content))
	expectedSha256 := hex.EncodeToString(sha[:])
	contentType := "image/jpeg"
	contentLength := int64(len(content))

	result, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(content)))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)

	require.NotEqual(t, uuid.Nil.String(), result.Asset.ID)
	require.Equal(t, "chat_attachment", result.Asset.Kind)
	require.Equal(t, contentType, result.Asset.ContentType)
	require.Equal(t, contentLength, result.Asset.ContentLength)
	require.Equal(t, expectedSha256, result.Asset.Sha256)
}

func TestService_UploadChatAttachment_Success_TextPlain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	content := "This is plain text content for chat"
	sha := sha256.Sum256([]byte(content))
	expectedSha256 := hex.EncodeToString(sha[:])
	contentType := "text/plain"
	contentLength := int64(len(content))

	result, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(content)))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)

	require.NotEqual(t, uuid.Nil.String(), result.Asset.ID)
	require.Equal(t, "chat_attachment", result.Asset.Kind)
	require.Equal(t, contentType, result.Asset.ContentType)
	require.Equal(t, contentLength, result.Asset.ContentLength)
	require.Equal(t, expectedSha256, result.Asset.Sha256)
}

func TestService_UploadChatAttachment_Success_TextCsv(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	content := "name,value\nfoo,bar\nbaz,qux"
	sha := sha256.Sum256([]byte(content))
	expectedSha256 := hex.EncodeToString(sha[:])
	contentType := "text/csv"
	contentLength := int64(len(content))

	result, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(content)))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)

	require.NotEqual(t, uuid.Nil.String(), result.Asset.ID)
	require.Equal(t, "chat_attachment", result.Asset.Kind)
	require.Equal(t, contentType, result.Asset.ContentType)
	require.Equal(t, contentLength, result.Asset.ContentLength)
	require.Equal(t, expectedSha256, result.Asset.Sha256)
}

func TestService_UploadChatAttachment_Success_ApplicationJson(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	content := `{"key": "value", "number": 42}`
	sha := sha256.Sum256([]byte(content))
	expectedSha256 := hex.EncodeToString(sha[:])
	contentType := "application/json"
	contentLength := int64(len(content))

	result, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(content)))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)

	require.NotEqual(t, uuid.Nil.String(), result.Asset.ID)
	require.Equal(t, "chat_attachment", result.Asset.Kind)
	require.Equal(t, contentType, result.Asset.ContentType)
	require.Equal(t, contentLength, result.Asset.ContentLength)
	require.Equal(t, expectedSha256, result.Asset.Sha256)
}

func TestService_UploadChatAttachment_Success_ApplicationYaml(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	content := "key: value\nnumber: 42"
	sha := sha256.Sum256([]byte(content))
	expectedSha256 := hex.EncodeToString(sha[:])
	contentType := "application/yaml"
	contentLength := int64(len(content))

	result, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(content)))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)

	require.NotEqual(t, uuid.Nil.String(), result.Asset.ID)
	require.Equal(t, "chat_attachment", result.Asset.Kind)
	require.Equal(t, contentType, result.Asset.ContentType)
	require.Equal(t, contentLength, result.Asset.ContentLength)
	require.Equal(t, expectedSha256, result.Asset.Sha256)
}

func TestService_UploadChatAttachment_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestAssetsService(t)

	// Create context without auth
	ctx := t.Context()

	_, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "text/plain",
		ContentLength:    100,
	}, io.NopCloser(strings.NewReader("test")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_UploadChatAttachment_NoContent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	_, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "text/plain",
		ContentLength:    0,
	}, io.NopCloser(strings.NewReader("")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "no content")
}

func TestService_UploadChatAttachment_ContentTooLarge(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	contentLength := int64(11 * 1024 * 1024) // 11MB, exceeds 10MB limit

	_, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "text/plain",
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader("test")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_UploadChatAttachment_UnsupportedContentType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	contentType := "application/octet-stream"
	contentLength := int64(100)

	_, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader("fake binary content")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnsupportedMedia, oopsErr.Code)
}

func TestService_UploadChatAttachment_DuplicateAsset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	content := "duplicate chat attachment content"
	contentType := "text/plain"
	contentLength := int64(len(content))

	// Upload the first attachment
	result1, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(content)))
	require.NoError(t, err)
	require.NotNil(t, result1)

	// Upload the same attachment again
	result2, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(content)))
	require.NoError(t, err)
	require.NotNil(t, result2)

	// Should return the same asset
	require.Equal(t, result1.Asset.ID, result2.Asset.ID)
	require.Equal(t, result1.Asset.Sha256, result2.Asset.Sha256)

	// Both should have the same URL
	expectedURL := fmt.Sprintf("%s?id=%s&project_id=%s", srv.ServeChatAttachmentAssetsPath(), result1.Asset.ID, projectID.String())
	require.Equal(t, expectedURL, result1.URL)
	require.Equal(t, expectedURL, result2.URL)
}

func TestService_UploadChatAttachment_InvalidContentType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	_, err := ti.service.UploadChatAttachment(ctx, &assets.UploadChatAttachmentForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "invalid-content-type",
		ContentLength:    100,
	}, io.NopCloser(strings.NewReader("test")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnsupportedMedia, oopsErr.Code)
}
