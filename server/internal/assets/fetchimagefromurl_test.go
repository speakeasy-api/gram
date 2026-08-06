package assets_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/assets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// fakePNG is a minimal payload served as image content in these tests. The
// handler does not decode image data, so any bytes work.
const fakePNG = "fake png image content"

func newImageServer(t *testing.T, contentType string, status int, body string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestService_FetchImageFromURL_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	srv := newImageServer(t, "image/png", http.StatusOK, fakePNG)

	sha := sha256.Sum256([]byte(fakePNG))
	expectedSha256 := hex.EncodeToString(sha[:])

	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)

	result, err := ti.service.FetchImageFromURL(ctx, &assets.FetchImageFromURLForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              srv.URL + "/icon.png",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Asset)
	require.NotEqual(t, uuid.Nil.String(), result.Asset.ID)
	require.Equal(t, "image", result.Asset.Kind)
	require.Equal(t, "image/png", result.Asset.ContentType)
	require.Equal(t, int64(len(fakePNG)), result.Asset.ContentLength)
	require.Equal(t, expectedSha256, result.Asset.Sha256)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestService_FetchImageFromURL_DeduplicatesByHash(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	srv := newImageServer(t, "image/png", http.StatusOK, fakePNG)

	first, err := ti.service.FetchImageFromURL(ctx, &assets.FetchImageFromURLForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              srv.URL + "/icon.png",
	})
	require.NoError(t, err)

	second, err := ti.service.FetchImageFromURL(ctx, &assets.FetchImageFromURLForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              srv.URL + "/icon.png",
	})
	require.NoError(t, err)

	require.Equal(t, first.Asset.ID, second.Asset.ID)
}

func TestService_FetchImageFromURL_ContentTypeFallbackFromExtension(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	// Server sends a generic content type; the .png extension resolves it.
	srv := newImageServer(t, "application/octet-stream", http.StatusOK, fakePNG)

	result, err := ti.service.FetchImageFromURL(ctx, &assets.FetchImageFromURLForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              srv.URL + "/icon.png",
	})

	require.NoError(t, err)
	require.Equal(t, "image/png", result.Asset.ContentType)
}

func TestService_FetchImageFromURL_FaviconICO(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	// Favicon fetches commonly return ICO — allowed on the URL-fetch path.
	srv := newImageServer(t, "image/x-icon", http.StatusOK, fakePNG)

	result, err := ti.service.FetchImageFromURL(ctx, &assets.FetchImageFromURLForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              srv.URL + "/favicon.ico",
	})

	require.NoError(t, err)
	require.Equal(t, "image/x-icon", result.Asset.ContentType)
}

func TestService_FetchImageFromURL_UndeterminableContentTypeDefaultsToPNG(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	// Neither the response content type nor the URL extension names an image
	// type. Rather than rejecting, the fetch assumes PNG: favicon hosts are
	// unreliable about content types, and refusing them would strand real
	// logos. Bytes are never decoded, so a non-image body is stored and later
	// fails to render rather than failing the fetch.
	srv := newImageServer(t, "text/html", http.StatusOK, "<html></html>")

	result, err := ti.service.FetchImageFromURL(ctx, &assets.FetchImageFromURLForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              srv.URL + "/page",
	})

	require.NoError(t, err)
	require.Equal(t, "image/png", result.Asset.ContentType)
}

func TestService_FetchImageFromURL_SVGRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	// SVG is not in the allowed image types (matches uploadImage), so the
	// sniff step rejects it even though the content type is image/*.
	srv := newImageServer(t, "image/svg+xml", http.StatusOK, "<svg></svg>")

	_, err := ti.service.FetchImageFromURL(ctx, &assets.FetchImageFromURLForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              srv.URL + "/icon.svg",
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnsupportedMedia, oopsErr.Code)
}

func TestService_FetchImageFromURL_UpstreamError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	srv := newImageServer(t, "image/png", http.StatusNotFound, "not found")

	_, err := ti.service.FetchImageFromURL(ctx, &assets.FetchImageFromURLForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              srv.URL + "/missing.png",
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_FetchImageFromURL_InvalidScheme(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAssetsService(t)

	_, err := ti.service.FetchImageFromURL(ctx, &assets.FetchImageFromURLForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              "ftp://example.com/icon.png",
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}

func TestService_FetchImageFromURL_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestAssetsService(t)
	ctx := t.Context()

	srv := newImageServer(t, "image/png", http.StatusOK, fakePNG)

	_, err := ti.service.FetchImageFromURL(ctx, &assets.FetchImageFromURLForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              srv.URL + "/icon.png",
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnauthorized, oopsErr.Code)
}
