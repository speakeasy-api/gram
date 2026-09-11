package admin

import (
	"bytes"
	"encoding/json"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	adminserver "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStandaloneImageRoutes_AuthenticateBeforeDecode(t *testing.T) {
	t.Parallel()
	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("subject", "operator@example.com")))
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	server := adminserver.New(gen.NewEndpoints(svc), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	for _, mount := range server.Mounts {
		if mount.Method != "UploadPlatformImage" {
			continue
		}
		for _, cookie := range []string{"", "invalid"} {
			req := httptest.NewRequest(mount.Verb, mount.Pattern, bytes.NewBufferString(`{`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer dashboard-key")
			req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: cookie})
			req = req.WithContext(contextvalues.SetAuthContext(req.Context(), &contextvalues.AuthContext{IsAdmin: true}))
			rec := httptest.NewRecorder()
			SessionMiddleware(mux).ServeHTTP(rec, req)
			require.Equal(t, http.StatusUnauthorized, rec.Code, mount.Method)
		}
	}
}

func TestStandaloneImageRoutes_ValidAdminDispatch(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	tp := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tp, nil)
	require.NoError(t, err)
	svc.SetAssetService(assets.NewPlatformService(svc.logger, tp, policy, db, assetstest.NewTestBlobStore(t)))
	sessionID, err := svc.sessions.Store(ctx, StoreParams{Email: "operator@example.com", Name: "Test Operator", OIDCSubject: "sub-admin", HD: testAdminHD, AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	// Binary SDK uploads use octet-stream; the server derives MIME and length.
	image := append([]byte{137, 80, 78, 71, 13, 10, 26, 10}, make([]byte, 32)...)
	req := httptest.NewRequest(http.MethodPost, "/admin/assets.uploadImage", bytes.NewReader(image))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec := httptest.NewRecorder()
	SessionMiddleware(mux).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var uploaded struct {
		Asset struct {
			ID          string `json:"id"`
			ContentType string `json:"content_type"`
		} `json:"asset"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &uploaded))
	require.Equal(t, "image/png", uploaded.Asset.ContentType)
	req = httptest.NewRequest(http.MethodGet, "/admin/assets.serveImage?id="+uploaded.Asset.ID, nil)
	rec = httptest.NewRecorder()
	SessionMiddleware(mux).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, image, rec.Body.Bytes())
	for _, tc := range []struct {
		name   string
		data   []byte
		status int
	}{
		{"over 4 MiB", make([]byte, assets.MaxFileSizeImage+1), http.StatusBadRequest},
		{"unsupported octet-stream", []byte("not an image"), http.StatusUnsupportedMediaType},
		{"empty octet-stream", nil, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/admin/assets.uploadImage", bytes.NewReader(tc.data))
			req.Header.Set("Content-Type", "application/octet-stream")
			req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
			rec := httptest.NewRecorder()
			SessionMiddleware(mux).ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
		})
	}
}
