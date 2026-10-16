package assets_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/assets"
	assetsvc "github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	return buf.Bytes()
}

func newPlatformImageUploads(t *testing.T, ti *testInstance) (*assetsvc.PlatformImageUploads, *http.ServeMux) {
	t.Helper()
	uploads := assetsvc.NewPlatformImageUploads(ti.service, testenv.NewMemoryCache(), &url.URL{Scheme: "https", Host: "admin.example.test"}, &url.URL{Scheme: "https", Host: "app.example.test"}, testenv.NewLogger(t))
	mux := http.NewServeMux()
	mux.Handle("PUT "+assetsvc.PlatformImageUploadPath, uploads.Handler())
	return uploads, mux
}

func putUpload(t *testing.T, mux *http.ServeMux, uploadURL string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	parsed, err := url.Parse(uploadURL)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPut, parsed.Path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

func TestPlatformImageUploads_StoresServableImageOnce(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	uploads, mux := newPlatformImageUploads(t, ti)
	image := testPNG(t)

	upload, err := uploads.Issue(ctx, "staff@example.test")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(upload.URL, "https://admin.example.test/admin/assets.uploadImage/"))

	response := putUpload(t, mux, upload.URL, image)
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

	require.Equal(t, http.StatusNotFound, putUpload(t, mux, upload.URL, image).Code, "an upload URL accepts one upload")
}

func TestPlatformImageUploads_RejectsUnknownTokensAndNonImages(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	uploads, mux := newPlatformImageUploads(t, ti)

	require.Equal(t, http.StatusNotFound, putUpload(t, mux, "https://admin.example.test/admin/assets.uploadImage/unknown", testPNG(t)).Code)

	svg, err := uploads.Issue(ctx, "staff@example.test")
	require.NoError(t, err)
	require.Equal(t, http.StatusUnsupportedMediaType, putUpload(t, mux, svg.URL, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)).Code)
	require.Equal(t, http.StatusNotFound, putUpload(t, mux, svg.URL, testPNG(t)).Code, "a rejected upload still spends the URL")

	large, err := uploads.Issue(ctx, "staff@example.test")
	require.NoError(t, err)
	require.Equal(t, http.StatusRequestEntityTooLarge, putUpload(t, mux, large.URL, make([]byte, assetsvc.MaxFileSizeImage+1)).Code)

	_, err = uploads.Issue(ctx, "")
	require.Error(t, err)
}
