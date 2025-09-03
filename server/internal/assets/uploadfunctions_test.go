package assets_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/assets"
	svc "github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_UploadFunctions_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	fixturePath := filepath.Clean(filepath.Join("fixtures", "valid-js.zip"))
	functionsContent, err := os.ReadFile(fixturePath)
	require.NoError(t, err)

	sha := sha256.Sum256(functionsContent)
	expectedSha256 := hex.EncodeToString(sha[:])
	contentType := "application/zip"
	contentLength := int64(len(functionsContent))

	result, err := ti.service.UploadFunctions(ctx, &assets.UploadFunctionsForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(bytes.NewBuffer(functionsContent)))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)

	require.NotEqual(t, uuid.Nil.String(), result.Asset.ID)
	require.Equal(t, "functions", result.Asset.Kind)
	require.Equal(t, contentType, result.Asset.ContentType)
	require.Equal(t, contentLength, result.Asset.ContentLength)
	require.Equal(t, expectedSha256, result.Asset.Sha256)
	require.NotEmpty(t, result.Asset.CreatedAt)
	require.NotEmpty(t, result.Asset.UpdatedAt)
}

func TestService_UploadFunctions_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestAssetsService(t)

	// Create context without auth
	ctx := t.Context()

	_, err := ti.service.UploadFunctions(ctx, &assets.UploadFunctionsForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/zip",
		ContentLength:    100,
	}, io.NopCloser(strings.NewReader("test")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_UploadFunctions_NoContent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	_, err := ti.service.UploadFunctions(ctx, &assets.UploadFunctionsForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/zip",
		ContentLength:    0,
	}, io.NopCloser(strings.NewReader("")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "no content")
}

func TestService_UploadFunctions_ContentTooLarge(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	contentLength := int64(svc.MaxFileSizeFunctions + 100)

	_, err := ti.service.UploadFunctions(ctx, &assets.UploadFunctionsForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/zip",
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader("test")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_UploadFunctions_UnsupportedContentType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	contentType := "image/png"
	contentLength := int64(100)

	_, err := ti.service.UploadFunctions(ctx, &assets.UploadFunctionsForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader("fake png content")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnsupportedMedia, oopsErr.Code)
}

func TestService_UploadFunctions_DuplicateAsset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	// Read valid fixture file
	fixturePath := filepath.Clean(filepath.Join("fixtures", "valid-py.zip"))
	functionsContent, err := os.ReadFile(fixturePath)
	require.NoError(t, err)

	contentType := "application/zip"
	contentLength := int64(len(functionsContent))

	// Upload the first functions package
	result1, err := ti.service.UploadFunctions(ctx, &assets.UploadFunctionsForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(bytes.NewBuffer(functionsContent)))
	require.NoError(t, err)
	require.NotNil(t, result1)

	// Upload the same functions package again
	result2, err := ti.service.UploadFunctions(ctx, &assets.UploadFunctionsForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(bytes.NewBuffer(functionsContent)))
	require.NoError(t, err)
	require.NotNil(t, result2)

	// Should return the same asset
	require.Equal(t, result1.Asset.ID, result2.Asset.ID)
	require.Equal(t, result1.Asset.Sha256, result2.Asset.Sha256)
}

func TestService_UploadFunctions_InvalidContentType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	_, err := ti.service.UploadFunctions(ctx, &assets.UploadFunctionsForm{
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

func TestService_UploadFunctions_SupportedContentTypes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	fixturePath := filepath.Clean(filepath.Join("fixtures", "valid-ts.zip"))
	functionsContent, err := os.ReadFile(fixturePath)
	require.NoError(t, err)

	supportedTypes := []struct {
		contentType string
		extension   string
	}{
		{"application/zip", ".zip"},
		{"application/x-zip-compressed", ".zip"},
		{"application/x-zip", ".zip"},
	}

	for _, tt := range supportedTypes {
		tt := tt
		t.Run(tt.contentType, func(t *testing.T) {
			t.Parallel()
			contentLength := int64(len(functionsContent))

			result, err := ti.service.UploadFunctions(ctx, &assets.UploadFunctionsForm{
				ApikeyToken:      nil,
				SessionToken:     nil,
				ProjectSlugInput: nil,
				ContentType:      tt.contentType,
				ContentLength:    contentLength,
			}, io.NopCloser(bytes.NewBuffer(functionsContent)))

			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, result.Asset)
			require.Equal(t, "functions", result.Asset.Kind)
			require.Equal(t, "application/zip", result.Asset.ContentType)
		})
	}
}

func TestService_UploadFunctions_InvalidArchive(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	fixturePath := filepath.Clean(filepath.Join("fixtures", "invalid.zip"))
	functionsContent, err := os.ReadFile(fixturePath)
	require.NoError(t, err)

	contentType := "application/zip"
	contentLength := int64(len(functionsContent))

	_, err = ti.service.UploadFunctions(ctx, &assets.UploadFunctionsForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(bytes.NewBuffer(functionsContent)))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "not a valid zip file")
}

func TestService_UploadFunctions_NoEntryPoint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	fixturePath := filepath.Clean(filepath.Join("fixtures", "no-entrypoint.zip"))
	functionsContent, err := os.ReadFile(fixturePath)
	require.NoError(t, err)

	contentType := "application/zip"
	contentLength := int64(len(functionsContent))

	_, err = ti.service.UploadFunctions(ctx, &assets.UploadFunctionsForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(bytes.NewBuffer(functionsContent)))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "no entry point found")
}
