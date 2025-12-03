package assets_test

import (
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestService_ServeImage_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	projectID := uuid.MustParse("c1b1b1b1-c1b1-c1b1-c1b1-c1b1b1b1c1b1")
	contentType := "image/png"
	contentLength := int64(len("fake image content"))
	testContent := "fake image content"
	filename := "test-asset.png"

	// Setup storage with test content first
	writer, uri, err := ti.storage.Write(ctx, filename, contentType, contentLength)
	require.NoError(t, err)

	_, err = io.Copy(writer, strings.NewReader(testContent))
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Create asset in database using the URI from storage
	asset, err := ti.repo.CreateAsset(ctx, repo.CreateAssetParams{
		Name:          filename,
		Url:           uri.String(),
		ProjectID:     projectID,
		Sha256:        "abc123",
		Kind:          "image",
		ContentType:   contentType,
		ContentLength: contentLength,
	})
	require.NoError(t, err)

	// Call ServeImage
	result, body, err := ti.service.ServeImage(ctx, &assets.ServeImageForm{
		ID: asset.ID.String(),
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, body)

	require.Equal(t, contentType, result.ContentType)
	require.Equal(t, contentLength, result.ContentLength)
	require.NotEmpty(t, result.LastModified)

	bodyBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, testContent, string(bodyBytes))

	err = body.Close()
	require.NoError(t, err)
}

func TestService_ServeImage_InvalidAssetID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	_, _, err := ti.service.ServeImage(ctx, &assets.ServeImageForm{
		ID: "invalid-uuid",
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_ServeImage_AssetNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	nonExistentID := uuid.New()

	_, _, err := ti.service.ServeImage(ctx, &assets.ServeImageForm{
		ID: nonExistentID.String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestService_ServeImage_FileNotInStorage(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	projectID := uuid.MustParse("c1b1b1b1-c1b1-c1b1-c1b1-c1b1b1b1c1b1")

	// Create asset in database but don't put file in storage
	asset, err := ti.repo.CreateAsset(ctx, repo.CreateAssetParams{
		Name:          "missing-image.png",
		Url:           "file://missing-asset.png",
		ProjectID:     projectID,
		Sha256:        "abc123",
		Kind:          "image",
		ContentType:   "image/png",
		ContentLength: 1024,
	})
	require.NoError(t, err)

	_, _, err = ti.service.ServeImage(ctx, &assets.ServeImageForm{
		ID: asset.ID.String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestService_ServeImage_InvalidAssetURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	projectID := uuid.MustParse("c1b1b1b1-c1b1-c1b1-c1b1-c1b1b1b1c1b1")

	// Create asset with invalid URL
	asset, err := ti.repo.CreateAsset(ctx, repo.CreateAssetParams{
		Name:          "invalid-url.png",
		Url:           "invalid-url",
		ProjectID:     projectID,
		Sha256:        "abc123",
		Kind:          "image",
		ContentType:   "image/png",
		ContentLength: 1024,
	})
	require.NoError(t, err)

	_, _, err = ti.service.ServeImage(ctx, &assets.ServeImageForm{
		ID: asset.ID.String(),
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
}
