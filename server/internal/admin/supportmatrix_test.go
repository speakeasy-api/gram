package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

// The seed only mirrors the axes, once, so rows that reference them by id
// have a target; the matrix itself is served from code.
func TestSupportMatrixHTTP(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/supportMatrix.get", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	sessionID, err := svc.sessions.Store(ctx, StoreParams{Email: "operator@example.com", Name: "Test Operator", OIDCSubject: "sub-admin", HD: testAdminHD, AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/admin/supportMatrix.get", nil)
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var snapshot gen.SupportMatrix
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &snapshot))
	require.NotEmpty(t, snapshot.Methods)
	require.Len(t, snapshot.Revision, 64)

	// The matrix is code: there is nothing to write to any more.
	req = httptest.NewRequest(http.MethodPost, "/admin/supportMatrix.update", strings.NewReader("{}"))
	req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}
