package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestStandaloneIssuerRoutes_StrictJSON(t *testing.T) {
	t.Parallel()
	svc := newTestSessionService(t, newTestOIDCClient(t, userinfoOK("strict-subject", "operator@example.com")))
	sessionID, err := svc.sessions.Store(t.Context(), StoreParams{Email: "operator@example.com", Name: "Test Operator", OIDCSubject: "strict-subject", HD: testAdminHD, AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	routes := []struct{ method, operation, body string }{
		{http.MethodPost, "createGlobalIssuer", `{"slug":"strict-test","issuer":"https://strict.example.com"}`},
		{http.MethodPost, "updateGlobalIssuer", `{"id":"00000000-0000-4000-8000-000000000001","name":"Strict test"}`},
		{http.MethodPost, "fetchGlobalIssuerMetadata", `{"issuer":"https://strict.example.com"}`},
		{http.MethodPost, "refreshGlobalIssuerMetadata", `{"id":"00000000-0000-4000-8000-000000000001"}`},
		{http.MethodPost, "migrateToGlobalIssuer", `{"source_id":"00000000-0000-4000-8000-000000000001","target_id":"00000000-0000-4000-8000-000000000002"}`},
	}
	for _, route := range routes {
		t.Run(route.operation, func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name, body string
				status     int
			}{
				{"oversized", route.body + strings.Repeat(" ", maxAdminJSONBodyBytes), http.StatusRequestEntityTooLarge},
				{"unknown field", strings.TrimSuffix(route.body, "}") + `,"misspelled_field":true}`, http.StatusBadRequest},
				{"trailing object", route.body + ` {}`, http.StatusBadRequest},
				{"trailing scalar", route.body + ` true`, http.StatusBadRequest},
				// No domain dependency is configured: 503 proves valid JSON reached the
				// actual adapter rather than being rejected by the transport. Business
				// success is covered separately by ValidAdminDispatch.
				{"valid dispatch", route.body, http.StatusServiceUnavailable},
				{"valid trailing whitespace", route.body + " \n\t", http.StatusServiceUnavailable},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					req := httptest.NewRequest(route.method, "/admin/remote-session-issuers."+route.operation, strings.NewReader(tc.body))
					req.Header.Set("Content-Type", "application/json")
					req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
					rec := httptest.NewRecorder()
					SessionMiddleware(mux).ServeHTTP(rec, req)
					require.Equal(t, tc.status, rec.Code, rec.Body.String())
				})
			}
		})
	}
}
