package mockworkos

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/speakeasy-api/gram/plog"
)

func newPortalTestHandler(t *testing.T) http.Handler {
	t.Helper()
	return NewHandler(
		Config{ExternalURL: "http://idp.example.com/"},
		plog.NewLogger(io.Discard),
		tracenoop.NewTracerProvider(),
		nil,
	).Handler()
}

func listCount(t *testing.T, h http.Handler, path, orgID string) int {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path+"?organization_id="+url.QueryEscape(orgID), nil)
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return len(body.Data)
}

func completePortal(t *testing.T, h http.Handler, orgID, intent string) *httptest.ResponseRecorder {
	t.Helper()
	query := url.Values{
		"intent":       {intent},
		"organization": {orgID},
		"success_url":  {"https://dashboard.example.com/setup/callback?intent=" + intent},
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/portal/complete?"+query.Encode(), nil)
	h.ServeHTTP(rec, req)
	return rec
}

func TestPortalCompletionGatesConnectionsAndDirectories(t *testing.T) {
	t.Parallel()

	h := newPortalTestHandler(t)
	const org = "org_devidp_acme"

	require.Equal(t, 0, listCount(t, h, "/connections", org), "fresh org has no SSO connection")
	require.Equal(t, 0, listCount(t, h, "/directories", org), "fresh org has no directory")

	rec := completePortal(t, h, org, "sso")
	require.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	require.Equal(t, "https://dashboard.example.com/setup/callback?intent=sso", rec.Header().Get("Location"))

	require.Equal(t, 1, listCount(t, h, "/connections", org), "sso completion creates the connection")
	require.Equal(t, 0, listCount(t, h, "/directories", org), "sso completion leaves directories alone")
	require.Equal(t, 0, listCount(t, h, "/connections", "org_devidp_other"), "other orgs are unaffected")

	completePortal(t, h, org, "dsync")
	require.Equal(t, 1, listCount(t, h, "/directories", org), "dsync completion links the directory")
}

func TestPortalCompleteRequiresAllParameters(t *testing.T) {
	t.Parallel()

	h := newPortalTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/portal/complete?intent=sso&organization=org_devidp_acme", nil)
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPortalPageLinksThroughCompleteEndpoint(t *testing.T) {
	t.Parallel()

	h := newPortalTestHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/portal?intent=sso&organization=org_devidp_acme&success_url=https%3A%2F%2Fdashboard.example.com%2Fcb%3Fa%3D1%26b%3D2", nil)
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `href="/mock-workos/portal/complete?intent=sso&amp;organization=org_devidp_acme&amp;success_url=https%3A%2F%2Fdashboard.example.com%2Fcb%3Fa%3D1%26b%3D2"`)
}

func TestGeneratePortalLinkUsesExternalURL(t *testing.T) {
	t.Parallel()

	h := newPortalTestHandler(t)
	rec := httptest.NewRecorder()
	body := `{"organization":"org_devidp_acme","intent":"sso","success_url":"https://dashboard.example.com/cb?a=1&b=2"}`
	req := httptest.NewRequest(http.MethodPost, "/portal/generate_link", strings.NewReader(body))
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var out struct {
		Link string `json:"link"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Equal(t,
		"http://idp.example.com/mock-workos/portal?intent=sso&organization=org_devidp_acme&success_url=https%3A%2F%2Fdashboard.example.com%2Fcb%3Fa%3D1%26b%3D2",
		out.Link)
}
