package mcp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
)

// A policy-denied endpoint's per-server metadata must answer exactly as a
// missing slug's, so the response cannot reveal that the endpoint exists.
func TestWellKnownMetadata_DeniedEndpointMatchesMissing(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	disabledToolset := createPublicMCPToolset(t, ctx, ti, authCtx, "well-known-disabled-"+uuid.NewString()[:8])
	disabledSlug := "well-known-disabled-endpoint-" + uuid.NewString()[:8]
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, disabledToolset.ID, disabledSlug, "disabled", uuid.NullUUID{}, uuid.Nil)

	for _, documentPath := range []string{wellknown.OAuthProtectedResourcePath, wellknown.OAuthAuthorizationServerPath} {
		t.Run(documentPath, func(t *testing.T) {
			t.Parallel()

			missing := httptest.NewRecorder()
			router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, documentPath+"/mcp/well-known-missing-"+uuid.NewString()[:8], nil))
			denied := httptest.NewRecorder()
			router.ServeHTTP(denied, httptest.NewRequest(http.MethodGet, documentPath+"/mcp/"+disabledSlug, nil))

			require.Equal(t, http.StatusNotFound, missing.Code, "body=%s", missing.Body.String())
			require.Contains(t, missing.Body.String(), publicMCPNotFoundMessage)
			require.Equal(t, missing.Code, denied.Code)
			require.Equal(t, missing.Header(), denied.Header())
			require.Equal(t, decodeErrorWithoutID(t, missing), decodeErrorWithoutID(t, denied))
		})
	}
}

// Routes that resolve a slug before answering must also answer a
// policy-denied endpoint exactly as a missing one.
func TestDeniedEndpointMatchesMissing_RegisterAndStreamRoutes(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	router := goahttp.NewMuxer()
	mcp.Attach(router, ti.service.Service, nil)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	disabledToolset := createPublicMCPToolset(t, ctx, ti, authCtx, "denied-routes-disabled-"+uuid.NewString()[:8])
	disabledSlug := "denied-routes-disabled-endpoint-" + uuid.NewString()[:8]
	createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, disabledToolset.ID, disabledSlug, "disabled", uuid.NullUUID{}, uuid.Nil)
	missingSlug := "denied-routes-missing-" + uuid.NewString()[:8]

	tests := []struct {
		name    string
		method  string
		suffix  string
		body    string
		headers map[string]string
	}{
		{
			name:    "register",
			method:  http.MethodPost,
			suffix:  "/register",
			body:    `{"redirect_uris":["http://127.0.0.1:3000/callback"],"client_name":"test"}`,
			headers: map[string]string{"Content-Type": "application/json"},
		},
		{
			name:    "get stream",
			method:  http.MethodGet,
			suffix:  "",
			body:    "",
			headers: map[string]string{"Accept": "text/event-stream"},
		},
		{
			name:    "delete session",
			method:  http.MethodDelete,
			suffix:  "",
			body:    "",
			headers: map[string]string{"Mcp-Session-Id": "session"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			serve := func(slug string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(tc.method, "/mcp/"+slug+tc.suffix, strings.NewReader(tc.body))
				for key, value := range tc.headers {
					req.Header.Set(key, value)
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				return w
			}

			missing := serve(missingSlug)
			denied := serve(disabledSlug)

			require.Equal(t, missing.Code, denied.Code, "missing=%s denied=%s", missing.Body.String(), denied.Body.String())
			require.Equal(t, missing.Header(), denied.Header())
			require.Equal(t, decodeBodyWithoutID(t, missing), decodeBodyWithoutID(t, denied))
		})
	}
}

// decodeBodyWithoutID decodes a JSON response body, dropping the HTTP error ID
// that is random per response.
func decodeBodyWithoutID(t *testing.T, w *httptest.ResponseRecorder) any {
	t.Helper()

	var body any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "body=%s", w.Body.String())
	if object, ok := body.(map[string]any); ok {
		delete(object, "ID")
	}
	return body
}

// decodeErrorWithoutID decodes an HTTP error body, dropping the error ID that
// is random per response.
func decodeErrorWithoutID(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "body=%s", w.Body.String())
	require.NotEmpty(t, body["ID"])
	delete(body, "ID")
	return body
}
