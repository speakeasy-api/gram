package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	adminserver "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/constants"
)

func TestGeneratedAdminRoutes_ExposeExactMigratedMounts(t *testing.T) {
	t.Parallel()

	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-mounts", "operator@example.com")))
	mux := goahttp.NewMuxer()
	server := adminserver.New(gen.NewEndpoints(svc), mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil)

	want := map[string]string{
		"GetSession":                          "GET /admin/session.get",
		"GetOrganizationFeatures":             "GET /admin/organization.features",
		"SetOrganizationFeature":              "POST /admin/organization.features",
		"GetOrganizationChatAnalysisSettings": "GET /admin/organization.chatAnalysisSettings",
		"SetOrganizationChatAnalysisSettings": "POST /admin/organization.chatAnalysisSettings",
		"TriggerOrganizationChatAnalysis":     "POST /admin/organization.chatAnalysisTrigger",
		"OpenOrganizationInDashboard":         "POST /admin/organization.open-dashboard",
	}
	got := map[string]string{}
	for _, mount := range server.Mounts {
		if _, ok := want[mount.Method]; ok {
			got[mount.Method] = mount.Verb + " " + mount.Pattern
		}
	}
	require.Equal(t, want, got)
}

func TestGeneratedAdminRoutes_AuthenticateBeforeDecode(t *testing.T) {
	t.Parallel()

	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-auth-first", "operator@example.com")))
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	unauthenticatedHandler := handler

	for name, request := range map[string]*http.Request{
		"session":          httptest.NewRequest(http.MethodGet, "/admin/session.get", nil),
		"features get":     httptest.NewRequest(http.MethodGet, "/admin/organization.features", nil),
		"features set":     httptest.NewRequest(http.MethodPost, "/admin/organization.features", bytes.NewBufferString(`{`)),
		"settings get":     httptest.NewRequest(http.MethodGet, "/admin/organization.chatAnalysisSettings", nil),
		"settings set":     httptest.NewRequest(http.MethodPost, "/admin/organization.chatAnalysisSettings", bytes.NewBufferString(`{`)),
		"analysis trigger": httptest.NewRequest(http.MethodPost, "/admin/organization.chatAnalysisTrigger", bytes.NewBufferString(`{`)),
		"open dashboard":   httptest.NewRequest(http.MethodPost, "/admin/organization.open-dashboard", nil),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			unauthenticatedHandler.ServeHTTP(rec, request)
			require.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}

	userinfoCalls := 0
	userinfo := userinfoOK("sub-auth-once", "operator@example.com")
	svc = newTestSessionService(t, newTestOIDCClient(t, func(w http.ResponseWriter, r *http.Request) {
		userinfoCalls++
		userinfo(w, r)
	}))
	sessionID, err := svc.sessions.Store(t.Context(), StoreParams{
		Email: "operator@example.com", Name: "Test Operator", OIDCSubject: "sub-auth-once", HD: testAdminHD,
		AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)
	mux = goahttp.NewMuxer()
	Attach(mux, svc)
	req := httptest.NewRequest(http.MethodPost, "/admin/organization.features", bytes.NewBufferString(`{`))
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec := httptest.NewRecorder()
	SessionMiddleware(mux).ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, 1, userinfoCalls, "successful pre-decode verification must be reused by Goa auth")
}

func TestGeneratedAdminRoutes_RejectOversizedJSONBody(t *testing.T) {
	t.Parallel()

	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("sub-oversized", "operator@example.com")))
	sessionID, err := svc.sessions.Store(t.Context(), StoreParams{
		Email: "operator@example.com", Name: "Test Operator", OIDCSubject: "sub-oversized", HD: testAdminHD,
		AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresAt: time.Now().Add(time.Hour),
	})
	require.NoError(t, err)

	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	req := httptest.NewRequest(http.MethodPost, "/admin/organization.features", bytes.NewReader(make([]byte, 1<<20+1)))
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec := httptest.NewRecorder()
	SessionMiddleware(mux).ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}
