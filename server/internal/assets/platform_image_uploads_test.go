package assets_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	admingen "github.com/speakeasy-api/gram/server/gen/admin_assets"
	assethttp "github.com/speakeasy-api/gram/server/gen/http/assets/server"
	goahttp "goa.design/goa/v3/http"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/assets"
	assetsvc "github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	return buf.Bytes()
}

func newPlatformImageUploadsWithCache(t *testing.T, ti *testInstance, uploadCache cache.Cache) (*assetsvc.PlatformImageUploads, http.Handler) {
	t.Helper()
	uploads, err := assetsvc.NewPlatformImageUploads(ti.service, uploadCache, &url.URL{Scheme: "https", Host: "admin.example.test"}, &url.URL{Scheme: "https", Host: "app.example.test"}, testenv.NewLogger(t))
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle("PUT "+assetsvc.PlatformImageUploadPath, uploads.Handler())
	// The admin server's Origin check runs first; a shell upload sends neither
	// Origin nor Referer.
	return uploads, middleware.AdminOriginCheck([]string{"https://admin-ui.example.test"})(mux)
}

func newPlatformImageUploads(t *testing.T, ti *testInstance) (*assetsvc.PlatformImageUploads, http.Handler) {
	t.Helper()
	return newPlatformImageUploadsWithCache(t, ti, testenv.NewMemoryCache())
}

func putUpload(t *testing.T, handler http.Handler, upload assetsvc.PlatformImageUpload, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPut, upload.URL, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if upload.Token != "" {
		request.Header.Set("Authorization", "Bearer "+upload.Token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestPlatformImageUploads_StoresServableImageOnce(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	uploads, mux := newPlatformImageUploads(t, ti)
	image := testPNG(t)

	upload, err := uploads.Issue(ctx, "staff@example.test")
	require.NoError(t, err)
	require.Equal(t, "https://admin.example.test/admin/assets.uploadImage", upload.URL, "the token stays out of the URL")
	require.NotEmpty(t, upload.Token)

	response := putUpload(t, mux, upload, image)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var stored assetsvc.StoredPlatformImage
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &stored))
	require.Equal(t, "image/png", stored.ContentType, "the type comes from the bytes, not the request header")
	require.Equal(t, "https://app.example.test/rpc/assets.serveImage?id="+stored.AssetID, stored.URL)

	row, err := ti.repo.GetPlatformAssetBySHA256(ctx, stored.SHA256)
	require.NoError(t, err)
	require.Equal(t, stored.AssetID, row.ID.String())
	require.False(t, row.OrganizationID.Valid)
	_, body, err := ti.service.ServeImage(ctx, &assets.ServeImageForm{ID: stored.AssetID})
	require.NoError(t, err)
	require.NoError(t, body.Close())

	require.Equal(t, http.StatusUnauthorized, putUpload(t, mux, upload, image).Code, "a token accepts one upload")
}

func TestPlatformImageUploads_RejectsUnknownTokensAndNonImages(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	uploads, mux := newPlatformImageUploads(t, ti)

	uploadURL := "https://admin.example.test" + assetsvc.PlatformImageUploadPath
	require.Equal(t, http.StatusUnauthorized, putUpload(t, mux, assetsvc.PlatformImageUpload{URL: uploadURL, Token: "unknown"}, testPNG(t)).Code)
	require.Equal(t, http.StatusUnauthorized, putUpload(t, mux, assetsvc.PlatformImageUpload{URL: uploadURL}, testPNG(t)).Code, "a request without a token is refused")

	svg, err := uploads.Issue(ctx, "staff@example.test")
	require.NoError(t, err)
	require.Equal(t, http.StatusUnsupportedMediaType, putUpload(t, mux, svg, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)).Code)
	require.Equal(t, http.StatusUnauthorized, putUpload(t, mux, svg, testPNG(t)).Code, "a rejected upload still spends the token")

	large, err := uploads.Issue(ctx, "staff@example.test")
	require.NoError(t, err)
	require.Equal(t, http.StatusRequestEntityTooLarge, putUpload(t, mux, large, make([]byte, assetsvc.MaxFileSizeImage+1)).Code)

	_, err = uploads.Issue(ctx, "")
	require.Error(t, err)
}

// failingRedeemCache stores grants but fails every redemption, as a Redis
// outage would.
type failingRedeemCache struct{ cache.Cache }

func (failingRedeemCache) GetAndDelete(context.Context, string, any) error {
	return errors.New("connection refused")
}

func TestPlatformImageUploads_CacheFailureIsNotAnUnknownToken(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	uploads, mux := newPlatformImageUploadsWithCache(t, ti, failingRedeemCache{Cache: testenv.NewMemoryCache()})

	upload, err := uploads.Issue(ctx, "staff@example.test")
	require.NoError(t, err)
	response := putUpload(t, mux, upload, testPNG(t))
	require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
}

func TestNewPlatformImageUploads_RequiresHTTPSExceptOnLoopback(t *testing.T) {
	t.Parallel()
	site := &url.URL{Scheme: "https", Host: "app.example.test"}
	for _, base := range []string{"https://admin.example.test", "http://localhost:8084", "http://127.0.0.1:8084", "http://[::1]:8084"} {
		parsed, err := url.Parse(base)
		require.NoError(t, err)
		_, err = assetsvc.NewPlatformImageUploads(nil, testenv.NewMemoryCache(), parsed, site, testenv.NewLogger(t))
		require.NoError(t, err, base)
	}
	for _, base := range []string{"http://admin.example.test", "http://10.0.0.1:8084", "http://user:pass@localhost:8084", "ftp://admin.example.test", "admin.example.test"} {
		parsed, err := url.Parse(base)
		require.NoError(t, err)
		_, err = assetsvc.NewPlatformImageUploads(nil, testenv.NewMemoryCache(), parsed, site, testenv.NewLogger(t))
		require.Error(t, err, base)
	}
}

func TestPlatformImageUploads_SVGCanonicalPublicResponse(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	raw := `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><!-- removed --><path fill="#123456" d="M0 0 L1 1"/></svg>`
	canonical := `<svg xmlns="http://www.w3.org/2000/svg"><path fill="#123456" d="M0 0 L1 1"></path></svg>`
	uploaded, err := ti.service.UploadPlatformImage(withAdmin(t, ctx), &admingen.UploadPlatformImageForm{ContentType: "image/svg+xml", ContentLength: int64(len(raw))}, io.NopCloser(strings.NewReader(raw)))
	require.NoError(t, err)
	require.Equal(t, "image/svg+xml", uploaded.Asset.ContentType)
	require.Equal(t, int64(len(canonical)), uploaded.Asset.ContentLength)
	require.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(canonical))), uploaded.Asset.Sha256)
	uploads, handler := newPlatformImageUploads(t, ti)
	for range 2 {
		grant, err := uploads.Issue(ctx, "staff@example.test")
		require.NoError(t, err)
		response := putUpload(t, handler, grant, []byte(raw))
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var stored assetsvc.StoredPlatformImage
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &stored))
		require.Equal(t, uploaded.Asset.ID, stored.AssetID)
		require.Equal(t, uploaded.Asset.Sha256, stored.SHA256)
		require.Equal(t, int64(len(canonical)), stored.Size)
		require.Equal(t, "image/svg+xml", stored.ContentType)
		require.Equal(t, http.StatusUnauthorized, putUpload(t, handler, grant, []byte(raw)).Code)
	}
	mux := goahttp.NewMuxer()
	assethttp.MountServeImageHandler(mux, assethttp.NewServeImageHandler(assets.NewServeImageEndpoint(ti.service), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/rpc/assets.serveImage?id="+uploaded.Asset.ID, nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Empty(t, response.Header().Get("Location"))
	require.Equal(t, "image/svg+xml", response.Header().Get("Content-Type"))
	require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "default-src 'none'; script-src 'none'; style-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; sandbox", response.Header().Get("Content-Security-Policy"))
	require.Equal(t, "cross-origin", response.Header().Get("Cross-Origin-Resource-Policy"))
	require.Equal(t, "*", response.Header().Get("Access-Control-Allow-Origin"))
	require.Equal(t, canonical, response.Body.String())
}

func TestPlatformImageUploads_InvalidBodies(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		body   []byte
		mime   string
		size   int64
		status int
	}{
		{"empty", nil, "image/svg+xml", 0, http.StatusUnsupportedMediaType},
		{"oversize", bytes.Repeat([]byte("x"), 4*1024*1024+1), "image/svg+xml", 4*1024*1024 + 1, http.StatusRequestEntityTooLarge},
		{"forged", testPNG(t), "image/svg+xml", -1, http.StatusUnsupportedMediaType},
		{"mismatch", testPNG(t), "image/png", 1, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAssetsService(t)
			uploads, mux := newPlatformImageUploads(t, ti)
			grant, err := uploads.Issue(ctx, "staff@example.test")
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPut, grant.URL, bytes.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+grant.Token)
			req.Header.Set("Content-Type", tc.mime)
			req.ContentLength = tc.size
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, req)
			require.Equal(t, tc.status, response.Code, response.Body.String())
		})
	}
}

type expiredGrantCache struct{ cache.Cache }

func (c expiredGrantCache) GetAndDelete(ctx context.Context, key string, value any) error {
	if err := c.Cache.GetAndDelete(ctx, key, value); err != nil {
		return fmt.Errorf("consume cached grant: %w", err)
	}
	if err := json.Unmarshal([]byte(`{"operator_email":"staff@example.test","expires_at":"2000-01-01T00:00:00Z"}`), value); err != nil {
		return fmt.Errorf("decode expired grant: %w", err)
	}
	return nil
}
func TestPlatformImageUploads_ExpiredGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	uploads, mux := newPlatformImageUploadsWithCache(t, ti, expiredGrantCache{testenv.NewMemoryCache()})
	grant, err := uploads.Issue(ctx, "staff@example.test")
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, putUpload(t, mux, grant, testPNG(t)).Code)
}

func TestServeImage_MountedRasterHeaders(t *testing.T) {
	t.Parallel()
	var jpegData bytes.Buffer
	require.NoError(t, jpeg.Encode(&jpegData, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil))
	for mime, data := range map[string][]byte{"image/png": testPNG(t), "image/jpeg": jpegData.Bytes()} {
		t.Run(mime, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAssetsService(t)
			uploaded, err := ti.service.UploadPlatformImage(withAdmin(t, ctx), &admingen.UploadPlatformImageForm{ContentType: mime, ContentLength: int64(len(data))}, io.NopCloser(bytes.NewReader(data)))
			require.NoError(t, err)
			mux := goahttp.NewMuxer()
			assethttp.MountServeImageHandler(mux, assethttp.NewServeImageHandler(assets.NewServeImageEndpoint(ti.service), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil))
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/rpc/assets.serveImage?id="+uploaded.Asset.ID, nil))
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, mime, response.Header().Get("Content-Type"))
			require.Equal(t, "nosniff", response.Header().Get("X-Content-Type-Options"))
			require.Empty(t, response.Header().Get("Content-Security-Policy"))
			require.Empty(t, response.Header().Get("Location"))
			require.Equal(t, data, response.Body.Bytes())
		})
	}
}
