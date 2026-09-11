package admin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/assets"
	assetsrepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	issuersrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestStandaloneIssuerLogo_SharedDashboardStorageAndReplacement(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	tp := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tp, nil)
	require.NoError(t, err)
	enc, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	svc.SetRemoteSessionService(remotesessions.NewGlobalService(svc.logger, tp, testenv.NewMeterProvider(t), db, enc, policy))

	// Separate service/store instances share only the database and disk location,
	// as the admin and dashboard API processes do. No copy or in-memory blob map.
	directory := t.TempDir()
	adminRoot, err := os.OpenRoot(directory)
	require.NoError(t, err)
	dashboardRoot, err := os.OpenRoot(directory)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, adminRoot.Close()); require.NoError(t, dashboardRoot.Close()) })
	svc.SetAssetService(assets.NewPlatformService(svc.logger, tp, policy, db, assets.NewFSBlobStore(svc.logger, adminRoot)))
	dashboardAssets := assets.NewPlatformService(svc.logger, tp, policy, db, assets.NewFSBlobStore(svc.logger, dashboardRoot))
	adminMux, dashboardMux := goahttp.NewMuxer(), goahttp.NewMuxer()
	Attach(adminMux, svc)
	assets.Attach(dashboardMux, dashboardAssets)
	adminHandler := SessionMiddleware(adminMux)
	sessionID, err := svc.sessions.Store(ctx, StoreParams{Email: "operator@example.com", Name: "Test Operator", OIDCSubject: "sub-admin", HD: testAdminHD, AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)

	request := func(path, contentType string, body []byte, authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		if authenticated {
			req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
		}
		rec := httptest.NewRecorder()
		adminHandler.ServeHTTP(rec, req)
		return rec
	}
	upload := func(data []byte) string {
		rec := request("/admin/assets.uploadImage", "application/octet-stream", data, true)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var result struct {
			Asset struct {
				ID string `json:"id"`
			} `json:"asset"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
		require.NotEmpty(t, result.Asset.ID)
		digest := sha256.Sum256(data)
		row, err := assetsrepo.New(db).GetPlatformAssetBySHA256(ctx, hex.EncodeToString(digest[:]))
		require.NoError(t, err)
		require.Equal(t, result.Asset.ID, row.ID.String())
		require.False(t, row.ProjectID.Valid)
		require.False(t, row.OrganizationID.Valid)
		require.Contains(t, row.Url, "/platform/")
		return result.Asset.ID
	}
	assertServed := func(id, mime string, data []byte) {
		for _, endpoint := range []struct {
			name, path string
			handler    http.Handler
		}{
			{"admin", "/admin/assets.serveImage?id=" + id, adminHandler},
			{"dashboard existing", "/rpc/assets.serveImage?id=" + id, dashboardMux},
		} {
			// Images remain public; neither admin cookies nor dashboard credentials
			// are introduced into the existing serving contract.
			rec := httptest.NewRecorder()
			endpoint.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, endpoint.path, nil))
			require.Equal(t, http.StatusOK, rec.Code, endpoint.name+": "+rec.Body.String())
			require.Equal(t, mime, rec.Header().Get("Content-Type"), endpoint.name)
			require.Equal(t, strconv.Itoa(len(data)), rec.Header().Get("Content-Length"), endpoint.name)
			require.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
			require.Equal(t, "cross-origin", rec.Header().Get("Cross-Origin-Resource-Policy"))
			require.Equal(t, data, rec.Body.Bytes(), endpoint.name)
		}
	}

	original := image.NewRGBA(image.Rect(0, 0, 2, 2))
	original.Set(0, 0, color.RGBA{R: 255, A: 255})
	var pngBody, jpegBody bytes.Buffer
	require.NoError(t, png.Encode(&pngBody, original))
	require.NoError(t, jpeg.Encode(&jpegBody, original, nil))
	rec := request("/admin/assets.uploadImage", "application/octet-stream", pngBody.Bytes(), false)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	firstID := upload(pngBody.Bytes())
	create, err := json.Marshal(map[string]string{"slug": "shared-logo-test", "issuer": "https://shared-logo.example.com", "logo_asset_id": firstID})
	require.NoError(t, err)
	rec = request("/admin/remote-session-issuers.createGlobalIssuer", "application/json", create, true)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var issuer struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &issuer))
	issuerID := uuid.MustParse(issuer.ID)
	row, err := issuersrepo.New(db).GetGlobalRemoteSessionIssuerByID(ctx, issuerID)
	require.NoError(t, err)
	require.True(t, row.LogoAssetID.Valid)
	require.Equal(t, uuid.MustParse(firstID), row.LogoAssetID.UUID)
	assertServed(firstID, "image/png", pngBody.Bytes())

	replacementID := upload(jpegBody.Bytes())
	require.NotEqual(t, firstID, replacementID)
	update, err := json.Marshal(map[string]string{"id": issuer.ID, "logo_asset_id": replacementID})
	require.NoError(t, err)
	rec = request("/admin/remote-session-issuers.updateGlobalIssuer", "application/json", update, true)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	row, err = issuersrepo.New(db).GetGlobalRemoteSessionIssuerByID(ctx, issuerID)
	require.NoError(t, err)
	require.Equal(t, uuid.MustParse(replacementID), row.LogoAssetID.UUID)
	assertServed(replacementID, "image/jpeg", jpegBody.Bytes())
	assertServed(firstID, "image/png", pngBody.Bytes())
	require.Equal(t, replacementID, upload(jpegBody.Bytes()), "same shared object is deduplicated, not copied")
}
