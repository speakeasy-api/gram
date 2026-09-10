package admin

import (
	"bytes"
	"encoding/json"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	adminserver "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStandaloneIssuerRoutes_AuthenticateBeforeDecode(t *testing.T) {
	t.Parallel()
	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("subject", "operator@example.com")))
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	server := adminserver.New(gen.NewEndpoints(svc), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)
	for _, mount := range server.Mounts {
		if !strings.HasPrefix(mount.Pattern, "/admin/remote-session-issuers.") && mount.Method != "UploadPlatformImage" {
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

func TestStandaloneIssuerRoutes_ValidAdminDispatch(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	enc, err := encryption.NewWithBytes(make([]byte, 32))
	require.NoError(t, err)
	tp := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tp, nil)
	require.NoError(t, err)
	var auditLog bytes.Buffer
	auditLogger := slog.New(slog.NewJSONHandler(&auditLog, nil))
	svc.SetRemoteSessionService(remotesessions.NewGlobalService(auditLogger, tp, testenv.NewMeterProvider(t), db, enc, policy))
	svc.SetAssetService(assets.NewPlatformService(svc.logger, tp, policy, db, assetstest.NewTestBlobStore(t)))
	sessionID, err := svc.sessions.Store(ctx, StoreParams{Email: "operator@example.com", Name: "Test Operator", OIDCSubject: "sub-admin", HD: testAdminHD, AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	req := httptest.NewRequest(http.MethodPost, "/admin/remote-session-issuers.createGlobalIssuer", bytes.NewBufferString(`{"slug":"standalone-test","issuer":"https://standalone.example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec := httptest.NewRecorder()
	SessionMiddleware(mux).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var issuer struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &issuer))
	require.NotEmpty(t, issuer.ID)
	var auditEntry map[string]any
	require.NoError(t, json.Unmarshal(auditLog.Bytes(), &auditEntry))
	require.Equal(t, "sub-admin", auditEntry["admin_oidc_subject"])
	require.Equal(t, "gram_admin", auditEntry["auth_source"])
	require.Equal(t, "operator@example.com", auditEntry[string(attr.AuthUserEmailKey)])
	require.Equal(t, "create", auditEntry[string(attr.AuditActionKey)])
	require.Equal(t, issuer.ID, auditEntry[string(attr.AuditSubjectIDKey)])
	require.NotContains(t, auditLog.String(), sessionID)
	req = httptest.NewRequest(http.MethodGet, "/admin/remote-session-issuers.getGlobalIssuer?id="+issuer.ID, nil)
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec = httptest.NewRecorder()
	SessionMiddleware(mux).ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "standalone-test")
	// Binary SDK uploads use octet-stream; the server derives MIME and length.
	image := append([]byte{137, 80, 78, 71, 13, 10, 26, 10}, make([]byte, 32)...)
	req = httptest.NewRequest(http.MethodPost, "/admin/assets.uploadImage", bytes.NewReader(image))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec = httptest.NewRecorder()
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
	// The trusted standalone context must not grant access to tenant methods.
	trusted := contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{SessionID: sessionID, OIDCSubject: "sub-admin", Email: "operator@example.com"})
	_, err = svc.remoteSessions.ListRemoteSessionIssuers(trusted, nil)
	require.Error(t, err)
}
