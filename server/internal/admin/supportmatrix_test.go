package admin

import (
	"bytes"
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

func TestSupportMatrixPersistence(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	require.NoError(t, SeedSupportMatrix(ctx, db))
	snapshot, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	require.Len(t, snapshot.Methods, 11)
	require.Len(t, snapshot.Products, 24)
	require.Len(t, snapshot.Capabilities, 12)
	require.Empty(t, snapshot.Draft.Mappings)
	require.Equal(t, "supported", snapshot.Draft.References["device"]["org"].Status)
	require.NoError(t, SeedSupportMatrix(ctx, db))
	same, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, snapshot.Revision, same.Revision)

	snapshot.Draft.Mappings["device/claude-code-web"] = &gen.SupportMapping{Applicability: "applicable", Conditions: "Team plan", Facts: map[string]*gen.SupportFact{"org": {Status: "partial", Note: "Requires setup", Verify: true}}}
	snapshot.Draft.References["device"]["org"] = &gen.SupportFact{Status: "partial", Note: "Reference caveat", Verify: true}
	saved, err := svc.UpdateSupportMatrix(ctx, &gen.UpdateSupportMatrixPayload{AdminSessionToken: nil, Revision: snapshot.Revision, Draft: snapshot.Draft})
	require.NoError(t, err)
	require.NotEqual(t, snapshot.Revision, saved.Revision)
	require.NoError(t, SeedSupportMatrix(ctx, db))
	loaded, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, saved, loaded)
	require.Equal(t, "Requires setup", loaded.Draft.Mappings["device/claude-code-web"].Facts["org"].Note)
	require.Equal(t, "Reference caveat", loaded.Methods[0].Facts["org"].Note)

	_, err = svc.UpdateSupportMatrix(ctx, &gen.UpdateSupportMatrixPayload{AdminSessionToken: nil, Revision: snapshot.Revision, Draft: snapshot.Draft})
	require.ErrorContains(t, err, "Support matrix changed")
	saved.Draft.Mappings["device/claude-code-web"].Facts["org"].Note = ""
	_, err = svc.UpdateSupportMatrix(ctx, &gen.UpdateSupportMatrixPayload{AdminSessionToken: nil, Revision: saved.Revision, Draft: saved.Draft})
	require.ErrorContains(t, err, "partial coverage requires notes")
	unchanged, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, loaded, unchanged)
}

func TestSupportMatrixHTTP(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	require.NoError(t, SeedSupportMatrix(ctx, db))
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	handler := SessionMiddleware(mux)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/admin/supportMatrix.get"
		if method == http.MethodPost {
			path = "/admin/supportMatrix.update"
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader("{}")))
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}
	sessionID, err := svc.sessions.Store(ctx, StoreParams{Email: "operator@example.com", Name: "Test Operator", OIDCSubject: "sub-admin", HD: testAdminHD, AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: constants.AdminSessionCookie, Value: sessionID})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	rec := request(http.MethodGet, "/admin/supportMatrix.get", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var snapshot gen.SupportMatrix
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &snapshot))
	for index, body := range []string{
		`{"revision":"` + snapshot.Revision + `","draft":{"mappings":{},"references":{}},"extra":true}`,
		`{"revision":"` + snapshot.Revision + `","draft":{"mappings":{"bad/platform":{"applicability":"applicable","conditions":"","facts":{}}},"references":{}}}`,
		`{"revision":"` + snapshot.Revision + `","draft":{"mappings":{},"references":{"device":{"org":{"status":"wrong","note":"","verify":false}}}}}`,
	} {
		rec = request(http.MethodPost, "/admin/supportMatrix.update", body)
		expected := http.StatusBadRequest
		if index == 1 {
			expected = http.StatusUnprocessableEntity
		}
		require.Equal(t, expected, rec.Code, rec.Body.String())
	}
	body := []byte(`{"revision":"` + snapshot.Revision + `","draft":{"mappings":{"device/claude-code-cli":{"applicability":"applicable","conditions":"","facts":{}}},"references":{}}}`)
	rec = request(http.MethodPost, "/admin/supportMatrix.update", string(body))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.True(t, bytes.Contains(rec.Body.Bytes(), []byte("device/claude-code-cli")))
	rec = request(http.MethodPost, "/admin/supportMatrix.update", string(body))
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}
