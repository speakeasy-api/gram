package assets_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
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
