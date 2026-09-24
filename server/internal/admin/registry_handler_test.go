package admin

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestRegistryMiddleware(t *testing.T) {
	t.Parallel()

	_, svc, _ := newTestAdminService(t)
	session, err := svc.sessions.Store(t.Context(), StoreParams{Email: "operator@example.com", Name: "Operator", OIDCSubject: "sub-admin", HD: testAdminHD, AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresAt: time.Now().Add(time.Hour)})
	require.NoError(t, err)
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	origins := []string{"https://admin.example.com"}
	handler := middleware.AdminCORS(origins)(middleware.AdminOriginCheck(origins)(SessionMiddleware(mux)))
	request := func(method, route, body, cookie, origin string) *httptest.ResponseRecorder {
		t.Helper()

		req := httptest.NewRequest(method, route, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: cookie, Value: session})
		}
		req.Header.Set("Authorization", "Bearer ordinary-customer-key")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	for _, cookie := range []string{"", constants.SessionCookie} {
		rec := request("POST", "/admin/registry.create", "not json", cookie, origins[0])
		require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	}
	for _, origin := range []string{"", "https://foreign.example.com"} {
		rec := request("POST", "/admin/registry.create", "{}", constants.AdminSessionCookie, origin)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	}
	listResponse := request("GET", "/admin/registry.list", "", constants.AdminSessionCookie, "")
	require.Equal(t, http.StatusOK, listResponse.Code, listResponse.Body.String())
	schemaResponse := request("GET", "/admin/registry.schema", "", constants.AdminSessionCookie, "")
	require.Equal(t, http.StatusNotFound, schemaResponse.Code, schemaResponse.Body.String())
	for _, route := range []string{"/admin/registry.get?id=bad", "/admin/registry.list?limit=-1", "/admin/registry.list?limit=51"} {
		rec := request("GET", route, "", constants.AdminSessionCookie, "")
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	for route, body := range map[string]string{
		"/admin/registry.save":         `{"id":"bad","updated_at":"token","data_json":"{}"}`,
		"/admin/registry.setPublished": `{"id":"bad","updated_at":"token","published":true}`,
	} {
		rec := request("POST", route, body, constants.AdminSessionCookie, origins[0])
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	for _, body := range []string{`{"data_json":"{}","unknown":true}`, `{"data_json":"{}"} {}`} {
		rec := request("POST", "/admin/registry.create", body, constants.AdminSessionCookie, origins[0])
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	}
	invalid := request("POST", "/admin/registry.create", `{"data_json":"{}"}`, constants.AdminSessionCookie, origins[0])
	require.Equal(t, http.StatusUnprocessableEntity, invalid.Code, invalid.Body.String())
	raw := `{"server":{"name":"example.test/admin","description":"Demo","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://example.test/mcp"}]},"extension":9007199254740993}`
	envelope, err := json.Marshal(map[string]string{"data_json": raw})
	require.NoError(t, err)
	created := request("POST", "/admin/registry.create", string(envelope), constants.AdminSessionCookie, origins[0])
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	var entry struct {
		ID        string `json:"id"`
		UpdatedAt string `json:"updated_at"`
	}
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &entry))
	for token, status := range map[string]int{"malformed": 400, "2020-01-01T00:00:00Z": 409, entry.UpdatedAt: 200} {
		body, err := json.Marshal(map[string]any{"id": entry.ID, "updated_at": token, "published": false})
		require.NoError(t, err)
		rec := request("POST", "/admin/registry.setPublished", string(body), constants.AdminSessionCookie, origins[0])
		require.Equal(t, status, rec.Code, rec.Body.String())
	}
	oversized := request("POST", "/admin/registry.create", string(bytes.Repeat([]byte(" "), (16<<20)+1)), constants.AdminSessionCookie, origins[0])
	require.Equal(t, http.StatusRequestEntityTooLarge, oversized.Code)
}

// An authentication failure must not consume even one byte of the request body.
type registryUnreadBody struct{ read bool }

func (b *registryUnreadBody) Read([]byte) (int, error) { b.read = true; return 0, io.EOF }

func (*registryUnreadBody) Close() error { return nil }

func TestRegistrySecurityAndPayloadBoundaries(t *testing.T) {
	t.Parallel()

	ctx, svc, db := newTestAdminService(t)
	store := func(expiry time.Time) string {
		t.Helper()

		id, err := svc.sessions.Store(t.Context(), StoreParams{Email: "operator@example.com", Name: "Operator", OIDCSubject: "sub-admin", HD: testAdminHD, AccessToken: "access-token", RefreshToken: "", ExpiresAt: expiry})
		require.NoError(t, err)
		return id
	}
	session := store(time.Now().Add(time.Hour))
	expired := store(time.Now().Add(-time.Hour))
	mux := goahttp.NewMuxer()
	Attach(mux, svc)
	origin := "https://admin.example.com"
	handler := middleware.AdminCORS([]string{origin})(middleware.AdminOriginCheck([]string{origin})(SessionMiddleware(mux)))
	send := func(method, route, token, cookie, ref string, body io.Reader) *httptest.ResponseRecorder {
		t.Helper()

		req := httptest.NewRequest(method, route, body)
		req.Header.Set("Content-Type", "application/json")
		if ref == "" {
			req.Header.Set("Origin", origin)
		} else {
			req.Header.Set("Referer", ref)
		}
		req.Header.Set("Authorization", "Bearer ordinary-customer-key")
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: cookie, Value: token})
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	for _, tc := range []struct{ token, cookie string }{{"", ""}, {session, constants.SessionCookie}, {"invalid", constants.AdminSessionCookie}, {expired, constants.AdminSessionCookie}} {
		body := &registryUnreadBody{read: false}
		rec := send("POST", "/admin/registry.create", tc.token, tc.cookie, "", body)
		require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		require.False(t, body.read)
		for _, route := range []string{"/admin/registry.list", "/admin/registry.get?id=bad"} {
			rec := send("GET", route, tc.token, tc.cookie, "", nil)
			require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		}
	}
	for ref, status := range map[string]int{origin + "/registry": 422, "https://foreign.example.com/registry": 403} {
		rec := send("POST", "/admin/registry.create", session, constants.AdminSessionCookie, ref, strings.NewReader(`{"data_json":"{}"}`))
		require.Equal(t, status, rec.Code, rec.Body.String())
	}
	encode := func(raw string) *bytes.Reader {
		t.Helper()

		b, err := json.Marshal(map[string]string{"data_json": raw})
		require.NoError(t, err)
		return bytes.NewReader(b)
	}
	secret := "rejected-private-value"
	invalid := `{"server":{"name":"example.test/invalid","description":"Demo","version":"1","remotes":[{"type":"` + secret + `","url":"https://example.test/mcp"}]}}`
	rec := send("POST", "/admin/registry.create", session, constants.AdminSessionCookie, "", encode(invalid))
	require.Equal(t, 422, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), secret)
	require.Contains(t, rec.Body.String(), "/server/remotes/0/type")
	prefix := `{"server":{"name":"example.test/large","description":"Demo","version":"1","remotes":[{"type":"streamable-http","url":"https://example.test/mcp"}]},"large_integer":9007199254740993,"extension":"`
	suffix := `"}`
	raw := prefix + strings.Repeat("x", (8<<20)-len(prefix)-len(suffix)) + suffix
	q := repo.New(db)
	storedSize, err := q.SerializedRegistryRecordBytes(ctx, []byte(raw))
	require.NoError(t, err)
	require.Greater(t, storedSize, int32(8<<20))
	accepted := prefix + strings.Repeat("x", (8<<20)-len(prefix)-len(suffix)-(int(storedSize)-len(raw))) + suffix
	storedSize, err = q.SerializedRegistryRecordBytes(ctx, []byte(accepted))
	require.NoError(t, err)
	require.EqualValues(t, 8<<20, storedSize)
	require.LessOrEqual(t, len(accepted), 8<<20)
	body := encode(accepted)
	require.Greater(t, body.Len(), 1<<20)
	require.Less(t, body.Len(), 16<<20)
	rec = send("POST", "/admin/registry.create", session, constants.AdminSessionCookie, "", body)
	require.Equal(t, 200, rec.Code, rec.Body.String()[:min(300, rec.Body.Len())])
	var entry struct {
		ID        string `json:"id"`
		UpdatedAt string `json:"updated_at"`
		DataJSON  string `json:"data_json"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &entry))
	require.Contains(t, entry.DataJSON, "9007199254740993")

	// The same raw-size-compliant record is rejected before either mutation.
	require.Len(t, raw, 8<<20)
	countBefore, err := q.CountRegistryEntries(ctx)
	require.NoError(t, err)
	rec = send("POST", "/admin/registry.create", session, constants.AdminSessionCookie, "", encode(raw))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "stored record exceeds serialized byte limit")
	countAfter, err := q.CountRegistryEntries(ctx)
	require.NoError(t, err)
	require.Equal(t, countBefore, countAfter)
	before := send("GET", "/admin/registry.get?id="+entry.ID, session, constants.AdminSessionCookie, "", nil)
	require.Equal(t, http.StatusOK, before.Code)
	saveBody, err := json.Marshal(map[string]string{"id": entry.ID, "updated_at": entry.UpdatedAt, "data_json": raw})
	require.NoError(t, err)
	rec = send("POST", "/admin/registry.save", session, constants.AdminSessionCookie, "", bytes.NewReader(saveBody))
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "stored record exceeds serialized byte limit")
	after := send("GET", "/admin/registry.get?id="+entry.ID, session, constants.AdminSessionCookie, "", nil)
	require.Equal(t, http.StatusOK, after.Code)
	require.JSONEq(t, before.Body.String(), after.Body.String())
	rec = send("POST", "/admin/registry.create", session, constants.AdminSessionCookie, "", encode(raw+" "))
	require.Equal(t, 422, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "record exceeds byte limit")
}
