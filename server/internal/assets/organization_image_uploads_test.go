package assets_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	testidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/speakeasy-api/gram/server/gen/assets"
	assetsvc "github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newOrganizationImageUploads(t *testing.T, ti *testInstance) (*assetsvc.OrganizationImageUploads, http.Handler) {
	t.Helper()
	uploads, err := assetsvc.NewOrganizationImageUploads(ti.service, testenv.NewMemoryCache(), &url.URL{Scheme: "https", Host: "api.example.test"}, &url.URL{Scheme: "https", Host: "app.example.test"}, testenv.NewLogger(t))
	require.NoError(t, err)
	mux := http.NewServeMux()
	mux.Handle("PUT "+assetsvc.OrganizationImageUploadPath, uploads.Handler())
	return uploads, mux
}

func organizationUpload(upload assetsvc.OrganizationImageUpload) assetsvc.PlatformImageUpload {
	return assetsvc.PlatformImageUpload(upload)
}

func TestOrganizationImageUploads_StoresOneOrganizationImagePerToken(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	uploads, mux := newOrganizationImageUploads(t, ti)
	image := testPNG(t)
	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)

	issued := time.Now()
	upload, err := uploads.Issue(ctx, testidp.MockOrgID, "user_uploader")
	require.NoError(t, err)
	require.Equal(t, "https://api.example.test/rpc/organizationAssets.uploadImage", upload.URL, "the token stays out of the URL")
	require.NotEmpty(t, upload.Token)
	require.NotContains(t, upload.URL, upload.Token)
	require.WithinDuration(t, issued.Add(assetsvc.OrganizationImageUploadTTL), upload.ExpiresAt, time.Minute)

	response := putUpload(t, mux, organizationUpload(upload), image)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var stored assetsvc.StoredPlatformImage
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &stored))
	require.Equal(t, "image/png", stored.ContentType, "the type comes from the bytes, not the request header")
	require.Equal(t, "https://app.example.test/rpc/assets.serveImage?id="+stored.AssetID, stored.URL)

	// The image belongs to the organization the token was issued for, in the
	// organization tier, and its creation is audited for the uploader.
	row, err := ti.repo.GetOrganizationAssetBySHA256(ctx, repo.GetOrganizationAssetBySHA256Params{OrganizationID: testidp.MockOrgID, Sha256: stored.SHA256})
	require.NoError(t, err)
	require.Equal(t, stored.AssetID, row.ID.String())
	require.False(t, row.ProjectID.Valid)
	require.Equal(t, testidp.MockOrgID, row.OrganizationID.String)
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAssetCreate)
	require.NoError(t, err)
	require.Equal(t, testidp.MockOrgID, record.OrganizationID)
	require.Equal(t, "user_uploader", record.ActorID)

	_, body, err := ti.service.ServeImage(ctx, &assets.ServeImageForm{ID: stored.AssetID})
	require.NoError(t, err)
	require.NoError(t, body.Close())

	require.Equal(t, http.StatusUnauthorized, putUpload(t, mux, organizationUpload(upload), image).Code, "a token accepts one upload")
}

func TestOrganizationImageUploads_RefusesExpiredUnknownAndNonImageUploads(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAssetsService(t)
	uploads, mux := newOrganizationImageUploads(t, ti)

	expired, err := uploads.Issue(ctx, testidp.MockOrgID, "user_uploader")
	require.NoError(t, err)
	uploads.SetClockForTest(func() time.Time { return time.Now().Add(assetsvc.OrganizationImageUploadTTL + time.Second) })
	require.Equal(t, http.StatusUnauthorized, putUpload(t, mux, organizationUpload(expired), testPNG(t)).Code, "an expired token is refused")
	uploads.SetClockForTest(time.Now)

	uploadURL := "https://api.example.test" + assetsvc.OrganizationImageUploadPath
	require.Equal(t, http.StatusUnauthorized, putUpload(t, mux, assetsvc.PlatformImageUpload{URL: uploadURL, Token: "unknown"}, testPNG(t)).Code)
	require.Equal(t, http.StatusUnauthorized, putUpload(t, mux, assetsvc.PlatformImageUpload{URL: uploadURL}, testPNG(t)).Code, "a request without a token is refused")

	svg, err := uploads.Issue(ctx, testidp.MockOrgID, "user_uploader")
	require.NoError(t, err)
	require.Equal(t, http.StatusUnsupportedMediaType, putUpload(t, mux, organizationUpload(svg), []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)).Code)
	require.Equal(t, http.StatusUnauthorized, putUpload(t, mux, organizationUpload(svg), testPNG(t)).Code, "a rejected upload still spends the token")

	large, err := uploads.Issue(ctx, testidp.MockOrgID, "user_uploader")
	require.NoError(t, err)
	require.Equal(t, http.StatusRequestEntityTooLarge, putUpload(t, mux, organizationUpload(large), make([]byte, assetsvc.MaxFileSizeImage+1)).Code)

	_, err = uploads.Issue(ctx, "", "user_uploader")
	require.Error(t, err, "a token is always bound to an organization")
	_, err = uploads.Issue(ctx, testidp.MockOrgID, "")
	require.Error(t, err, "a token is always bound to an uploader")
}

func TestNewOrganizationImageUploads_RequiresHTTPSExceptOnLoopback(t *testing.T) {
	t.Parallel()
	site := &url.URL{Scheme: "https", Host: "app.example.test"}
	for _, base := range []string{"https://api.example.test", "http://localhost:8080"} {
		parsed, err := url.Parse(base)
		require.NoError(t, err)
		_, err = assetsvc.NewOrganizationImageUploads(nil, testenv.NewMemoryCache(), parsed, site, testenv.NewLogger(t))
		require.NoError(t, err, base)
	}
	for _, base := range []string{"http://api.example.test", "ftp://api.example.test"} {
		parsed, err := url.Parse(base)
		require.NoError(t, err)
		_, err = assetsvc.NewOrganizationImageUploads(nil, testenv.NewMemoryCache(), parsed, site, testenv.NewLogger(t))
		require.Error(t, err, base)
	}
}
