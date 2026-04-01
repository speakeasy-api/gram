package assets_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_UploadImage_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	imageContent := "fake png image content"
	sha := sha256.Sum256([]byte(imageContent))
	expectedSha256 := hex.EncodeToString(sha[:])
	contentType := "image/png"
	contentLength := int64(len(imageContent))
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)

	result, err := ti.service.UploadImage(ctx, &assets.UploadImageForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(imageContent)))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)

	require.NotEqual(t, uuid.Nil.String(), result.Asset.ID)
	require.Equal(t, "image", result.Asset.Kind)
	require.Equal(t, contentType, result.Asset.ContentType)
	require.Equal(t, contentLength, result.Asset.ContentLength)
	require.Equal(t, expectedSha256, result.Asset.Sha256)
	require.NotEmpty(t, result.Asset.CreatedAt)
	require.NotEmpty(t, result.Asset.UpdatedAt)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestService_UploadImage_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestAssetsService(t)
	ctx := t.Context()
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)

	// Create context without auth
	_, err = ti.service.UploadImage(ctx, &assets.UploadImageForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "image/png",
		ContentLength:    100,
	}, io.NopCloser(strings.NewReader("test")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestService_UploadImage_NoContent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	_, err := ti.service.UploadImage(ctx, &assets.UploadImageForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "image/png",
		ContentLength:    0,
	}, io.NopCloser(strings.NewReader("")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "no content")
}

func TestService_UploadImage_ContentTooLarge(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	contentLength := int64(5 * 1024 * 1024) // 5MB, exceeds 4MB limit

	_, err := ti.service.UploadImage(ctx, &assets.UploadImageForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "image/png",
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader("test")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_UploadImage_UnsupportedContentType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	contentType := "application/pdf"
	contentLength := int64(100)

	_, err := ti.service.UploadImage(ctx, &assets.UploadImageForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader("fake pdf content")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnsupportedMedia, oopsErr.Code)
}

func TestService_UploadImage_DuplicateAsset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	imageContent := "duplicate image content"
	contentType := "image/png"
	contentLength := int64(len(imageContent))
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)

	// Upload the first image
	result1, err := ti.service.UploadImage(ctx, &assets.UploadImageForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(imageContent)))
	require.NoError(t, err)
	require.NotNil(t, result1)
	afterFirstCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterFirstCount)

	// Upload the same image again
	result2, err := ti.service.UploadImage(ctx, &assets.UploadImageForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(imageContent)))
	require.NoError(t, err)
	require.NotNil(t, result2)

	// Should return the same asset
	require.Equal(t, result1.Asset.ID, result2.Asset.ID)
	require.Equal(t, result1.Asset.Sha256, result2.Asset.Sha256)

	afterSecondCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)
	require.Equal(t, afterFirstCount, afterSecondCount)
}

func TestService_UploadImage_InvalidContentType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	_, err := ti.service.UploadImage(ctx, &assets.UploadImageForm{
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

func TestService_UploadImage_AuditLogMetadata(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)
	imageContent := "audit image content"
	contentType := "image/png"
	contentLength := int64(len(imageContent))
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)

	result, err := ti.service.UploadImage(ctx, &assets.UploadImageForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(imageContent)))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	require.NotNil(t, authCtx.ProjectID)

	assetID, err := uuid.Parse(result.Asset.ID)
	require.NoError(t, err)

	storedAsset, err := ti.repo.GetProjectAsset(ctx, repo.GetProjectAssetParams{
		ProjectID: *authCtx.ProjectID,
		ID:        assetID,
	})
	require.NoError(t, err)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionAssetCreate), record.Action)
	require.Equal(t, "asset", record.SubjectType)
	require.Equal(t, storedAsset.Name, record.SubjectDisplay)
	require.Empty(t, record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)

	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, urn.NewAsset(urn.AssetKindImage, assetID).String(), metadata["asset_urn"])

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}
