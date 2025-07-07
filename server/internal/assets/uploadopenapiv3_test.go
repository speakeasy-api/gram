package assets_test

import (
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/assets"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_UploadOpenAPIv3_Success_YAML(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	yamlContent := `openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0
paths:
  /test:
    get:
      summary: Test endpoint
      responses:
        '200':
          description: Success`
	contentType := "application/yaml"
	contentLength := int64(len(yamlContent))

	result, err := ti.service.UploadOpenAPIv3(ctx, &assets.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(yamlContent)))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)

	require.NotEqual(t, uuid.Nil.String(), result.Asset.ID)
	require.Equal(t, "openapiv3", result.Asset.Kind)
	require.Equal(t, contentType, result.Asset.ContentType)
	require.Equal(t, contentLength, result.Asset.ContentLength)
	require.NotEmpty(t, result.Asset.Sha256)
	require.NotEmpty(t, result.Asset.CreatedAt)
	require.NotEmpty(t, result.Asset.UpdatedAt)
}

func TestService_UploadOpenAPIv3_Success_JSON(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	jsonContent := `{
  "openapi": "3.0.0",
  "info": {
    "title": "Test API",
    "version": "1.0.0"
  },
  "paths": {
    "/test": {
      "get": {
        "summary": "Test endpoint",
        "responses": {
          "200": {
            "description": "Success"
          }
        }
      }
    }
  }
}`
	contentType := "application/json"
	contentLength := int64(len(jsonContent))

	result, err := ti.service.UploadOpenAPIv3(ctx, &assets.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(jsonContent)))

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)

	require.Equal(t, "openapiv3", result.Asset.Kind)
	require.Equal(t, contentType, result.Asset.ContentType)
	require.Equal(t, contentLength, result.Asset.ContentLength)
}

func TestService_UploadOpenAPIv3_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestAssetsService(t)

	// Create context without auth
	ctx := t.Context()

	_, err := ti.service.UploadOpenAPIv3(ctx, &assets.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/yaml",
		ContentLength:    100,
	}, io.NopCloser(strings.NewReader("test")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}

func TestService_UploadOpenAPIv3_NoContent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	_, err := ti.service.UploadOpenAPIv3(ctx, &assets.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/yaml",
		ContentLength:    0,
	}, io.NopCloser(strings.NewReader("")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_UploadOpenAPIv3_ContentTooLarge(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	contentLength := int64(11 * 1024 * 1024) // 11MB, exceeds 10MB limit

	_, err := ti.service.UploadOpenAPIv3(ctx, &assets.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      "application/yaml",
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader("test")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_UploadOpenAPIv3_UnsupportedContentType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	contentType := "application/xml"
	contentLength := int64(100)

	_, err := ti.service.UploadOpenAPIv3(ctx, &assets.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader("fake xml content")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnsupportedMedia, oopsErr.Code)
}

func TestService_UploadOpenAPIv3_DuplicateAsset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	yamlContent := `openapi: 3.0.0
info:
  title: Duplicate API
  version: 1.0.0`
	contentType := "application/yaml"
	contentLength := int64(len(yamlContent))

	// Upload the first spec
	result1, err := ti.service.UploadOpenAPIv3(ctx, &assets.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(yamlContent)))
	require.NoError(t, err)
	require.NotNil(t, result1)

	// Upload the same spec again
	result2, err := ti.service.UploadOpenAPIv3(ctx, &assets.UploadOpenAPIv3Form{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		ContentType:      contentType,
		ContentLength:    contentLength,
	}, io.NopCloser(strings.NewReader(yamlContent)))
	require.NoError(t, err)
	require.NotNil(t, result2)

	// Should return the same asset
	require.Equal(t, result1.Asset.ID, result2.Asset.ID)
	require.Equal(t, result1.Asset.Sha256, result2.Asset.Sha256)
}

func TestService_UploadOpenAPIv3_InvalidContentType(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	_, err := ti.service.UploadOpenAPIv3(ctx, &assets.UploadOpenAPIv3Form{
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
