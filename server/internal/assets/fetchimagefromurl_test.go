package assets_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/assets"
	svc "github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// pngBytes is a real 1x1 PNG: the stored content type is sniffed from the
// bytes, so a placeholder string would be rejected as a non-image.
func pngBytes(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	return buf.Bytes()
}

// The fetch path only accepts https, so the upstream has to be a TLS server
// whose cert the test policy trusts (see unsafeFetchPolicy).
func newImageServer(t *testing.T, contentType string, status int, body []byte) *httptest.Server {
	t.Helper()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestService_FetchImageFromURL_Success(t *testing.T) {
	t.Parallel()

	body := pngBytes(t)
	srv := newImageServer(t, "image/png", http.StatusOK, body)

	ctx, ti := newTestAssetsServiceWithPolicy(t, unsafeFetchPolicy(t, []string{}, nil, srv))

	sha := sha256.Sum256(body)
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
	require.Equal(t, int64(len(body)), result.Asset.ContentLength)
	require.Equal(t, expectedSha256, result.Asset.Sha256)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestService_FetchImageFromURL_DeduplicatesByHash(t *testing.T) {
	t.Parallel()

	srv := newImageServer(t, "image/png", http.StatusOK, pngBytes(t))

	ctx, ti := newTestAssetsServiceWithPolicy(t, unsafeFetchPolicy(t, []string{}, nil, srv))

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

func TestService_FetchImageFromURL_ContentTypeDetectedFromBytes(t *testing.T) {
	t.Parallel()

	// Neither the declared type nor the extension identifies the image.
	srv := newImageServer(t, "application/octet-stream", http.StatusOK, pngBytes(t))

	ctx, ti := newTestAssetsServiceWithPolicy(t, unsafeFetchPolicy(t, []string{}, nil, srv))

	result, err := ti.service.FetchImageFromURL(ctx, &assets.FetchImageFromURLForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              srv.URL + "/icon",
	})

	require.NoError(t, err)
	require.Equal(t, "image/png", result.Asset.ContentType)
}

func TestService_FetchImageFromURL_NonImageRejected(t *testing.T) {
	t.Parallel()

	// An HTML error page served with a 200 and an image extension.
	srv := newImageServer(t, "image/png", http.StatusOK, []byte("<html></html>"))

	ctx, ti := newTestAssetsServiceWithPolicy(t, unsafeFetchPolicy(t, []string{}, nil, srv))

	_, err := ti.service.FetchImageFromURL(ctx, &assets.FetchImageFromURLForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              srv.URL + "/icon.png",
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnsupportedMedia, oopsErr.Code)
}

func TestService_FetchImageFromURL_SVGRejected(t *testing.T) {
	t.Parallel()

	// Not in the allowed image types, matching uploadImage.
	srv := newImageServer(t, "image/svg+xml", http.StatusOK, []byte("<svg></svg>"))

	ctx, ti := newTestAssetsServiceWithPolicy(t, unsafeFetchPolicy(t, []string{}, nil, srv))

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

func TestService_FetchImageFromURL_ContentTooLarge(t *testing.T) {
	t.Parallel()

	// Icon hosts often stream without a Content-Length, so the declared-length
	// check cannot reject the download up front; the read-one-byte-past-the-cap
	// guard after the limited copy has to catch it instead. Flushing before the
	// body forces chunked encoding so no Content-Length is ever declared.
	oversized := make([]byte, svc.MaxFileSizeImage+1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = w.Write(oversized)
	}))
	t.Cleanup(srv.Close)

	ctx, ti := newTestAssetsServiceWithPolicy(t, unsafeFetchPolicy(t, []string{}, nil, srv))

	_, err := ti.service.FetchImageFromURL(ctx, &assets.FetchImageFromURLForm{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		URL:              srv.URL + "/icon.png",
	})

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	require.Contains(t, oopsErr.Error(), "content exceeds size limit")
}

func TestService_FetchImageFromURL_UpstreamError(t *testing.T) {
	t.Parallel()

	srv := newImageServer(t, "image/png", http.StatusNotFound, []byte("not found"))

	ctx, ti := newTestAssetsServiceWithPolicy(t, unsafeFetchPolicy(t, []string{}, nil, srv))

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

	srv := newImageServer(t, "image/png", http.StatusOK, pngBytes(t))

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
