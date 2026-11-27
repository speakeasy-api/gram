package mcp_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// unmatchedMCPMetadataMessage is the message a discovery lookup for an
// unmatched MCP URL answers with.
const unmatchedMCPMetadataMessage = "oauth metadata not found: no OAuth or OpenID metadata is published at this URL"

// unmatchedMCPTransportMessage is the message an MCP request to an unmatched
// MCP URL answers with.
const unmatchedMCPTransportMessage = "mcp server not found: no MCP server is served at this URL. Check that the URL has no extra path segment, or copy the server's current URL from the MCP page in the Speakeasy dashboard and update your MCP client configuration"

// unmatchedRouteMiddlewareHeader marks responses that passed through the mux
// middleware chain.
const unmatchedRouteMiddlewareHeader = "X-Test-Middleware"

// newUnmatchedRouteMux builds a mux the way the server does: middleware first,
// then Attach and the not-found handler, then further routes.
func newUnmatchedRouteMux(t *testing.T) goahttp.Muxer {
	t.Helper()

	_, ti := newTestMCPService(t)

	mux := goahttp.NewMuxer()
	mux.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set(unmatchedRouteMiddlewareHeader, "1")
			next.ServeHTTP(w, r)
		})
	})
	mcp.Attach(mux, ti.service.Service, nil)
	setMCPNotFoundHandler(t, mux)
	mux.Handle(http.MethodGet, "/registered-after-attach", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	return mux
}

// setMCPNotFoundHandler installs mcp.NotFoundHandler on mux as the server
// listeners do.
func setMCPNotFoundHandler(t *testing.T, mux goahttp.Muxer) {
	t.Helper()

	router, ok := mux.(interface{ NotFound(http.HandlerFunc) })
	require.True(t, ok, "mux %T does not support a not-found handler", mux)
	router.NotFound(mcp.NotFoundHandler(testenv.NewLogger(t)))
}

func TestUnmatchedMCPRoute_TransportRequestAnswersJSONRPCNotFound(t *testing.T) {
	t.Parallel()

	mux := newUnmatchedRouteMux(t)

	tests := []struct {
		name    string
		method  string
		path    string
		body    io.Reader
		headers map[string]string
	}{
		{
			name:    "post json initialize below a slug",
			method:  http.MethodPost,
			path:    "/mcp/bad-slug/extra",
			body:    bytes.NewReader(makeInitializeBody()),
			headers: map[string]string{"Content-Type": "application/json", "Accept": "*/*"},
		},
		{
			name:    "post json with charset",
			method:  http.MethodPost,
			path:    "/mcp/bad-slug/extra",
			body:    bytes.NewReader(makeInitializeBody()),
			headers: map[string]string{"Content-Type": "application/json; charset=utf-8"},
		},
		{
			name:    "get stream with protocol version",
			method:  http.MethodGet,
			path:    "/mcp/bad-slug/extra",
			body:    nil,
			headers: map[string]string{mcpversions.HTTPHeader: mcpversions.Version20251125, "Accept": "text/event-stream"},
		},
		{
			name:    "get stream without protocol headers",
			method:  http.MethodGet,
			path:    "/mcp/bad-slug/extra",
			body:    nil,
			headers: map[string]string{"Accept": "text/event-stream"},
		},
		{
			name:    "delete with session",
			method:  http.MethodDelete,
			path:    "/mcp/bad-slug/extra",
			body:    nil,
			headers: map[string]string{"Mcp-Session-Id": "session"},
		},
		{
			name:    "post json with empty slug",
			method:  http.MethodPost,
			path:    "/mcp//",
			body:    bytes.NewReader(makeInitializeBody()),
			headers: map[string]string{"Content-Type": "application/json"},
		},
		{
			name:    "post json to bare mcp",
			method:  http.MethodPost,
			path:    "/mcp",
			body:    bytes.NewReader(makeInitializeBody()),
			headers: map[string]string{"Content-Type": "application/json"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tc.method, tc.path, tc.body)
			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			require.Equal(t, http.StatusNotFound, w.Code)
			require.Equal(t, "application/json", w.Header().Get("Content-Type"))
			require.Equal(t, "1", w.Header().Get(unmatchedRouteMiddlewareHeader))

			var response map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response), "response body: %s", w.Body.String())
			require.Equal(t, "2.0", response["jsonrpc"])
			id, hasID := response["id"]
			require.True(t, hasID, "expected a JSON-RPC id: %v", response)
			require.Nil(t, id)

			errorBody, ok := response["error"].(map[string]any)
			require.True(t, ok, "expected JSON-RPC error: %v", response)
			require.InDelta(t, -32002, errorBody["code"], 0)
			require.Equal(t, unmatchedMCPTransportMessage, errorBody["message"])
		})
	}
}

func TestUnmatchedMCPRoute_DiscoveryAnswersJSONNotFound(t *testing.T) {
	t.Parallel()

	mux := newUnmatchedRouteMux(t)

	paths := []string{
		"/mcp/bad-slug/.well-known/oauth-protected-resource",
		"/mcp/bad-slug/.well-known/oauth-authorization-server",
		"/mcp/bad-slug/.well-known/openid-configuration",
		"/mcp/bad-slug/.well-known/oauth-protected-resource/",
		"/mcp/gateway/bad-team/.well-known/oauth-protected-resource",
		"/.well-known/oauth-protected-resource/mcp",
		"/.well-known/oauth-protected-resource/mcp/bad-slug/",
		"/.well-known/oauth-protected-resource/mcp/bad-slug/extra",
		"/.well-known/oauth-authorization-server/mcp/bad-slug/",
		"/.well-known/openid-configuration/mcp/bad-slug",
	}

	for _, path := range paths {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+" "+path, func(t *testing.T) {
				t.Parallel()

				req := httptest.NewRequest(method, path, nil)
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, req)

				require.Equal(t, http.StatusNotFound, w.Code)
				require.Equal(t, "application/json", w.Header().Get("Content-Type"))
				require.Equal(t, "1", w.Header().Get(unmatchedRouteMiddlewareHeader))
				if method == http.MethodGet {
					require.True(t, json.Valid(w.Body.Bytes()), "response body: %s", w.Body.String())
					require.Contains(t, w.Body.String(), unmatchedMCPMetadataMessage)
				}
			})
		}
	}
}

func TestUnmatchedMCPRoute_OtherRequestsKeepPlainNotFound(t *testing.T) {
	t.Parallel()

	mux := newUnmatchedRouteMux(t)

	tests := []struct {
		name    string
		method  string
		path    string
		headers map[string]string
	}{
		{name: "browser get below a slug", method: http.MethodGet, path: "/mcp/bad-slug/extra", headers: map[string]string{"Accept": "text/html"}},
		{name: "form post below a slug", method: http.MethodPost, path: "/mcp/bad-slug/extra", headers: map[string]string{"Content-Type": "application/x-www-form-urlencoded"}},
		{name: "discovery post below a slug", method: http.MethodPost, path: "/mcp/bad-slug/.well-known/oauth-protected-resource", headers: nil},
		{name: "json post outside mcp", method: http.MethodPost, path: "/unknown", headers: map[string]string{"Content-Type": "application/json"}},
		{name: "discovery outside mcp", method: http.MethodGet, path: "/x/.well-known/oauth-protected-resource", headers: map[string]string{mcpversions.HTTPHeader: mcpversions.Version20251125}},
		{name: "mcp prefix without separator", method: http.MethodPost, path: "/mcpextra/slug", headers: map[string]string{"Content-Type": "application/json"}},
		{name: "browser get bare mcp", method: http.MethodGet, path: "/mcp", headers: map[string]string{"Accept": "text/html"}},
		{name: "json post to path-inserted discovery", method: http.MethodPost, path: "/.well-known/oauth-protected-resource/mcp/bad-slug/", headers: map[string]string{"Content-Type": "application/json"}},
		{name: "path-inserted discovery without separator", method: http.MethodGet, path: "/.well-known/oauth-protected-resource/mcpextra", headers: nil},
		{name: "root discovery", method: http.MethodGet, path: "/.well-known/oauth-protected-resource", headers: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tc.method, tc.path, nil)
			for key, value := range tc.headers {
				req.Header.Set(key, value)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			require.Equal(t, http.StatusNotFound, w.Code)
			require.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
			require.Equal(t, "404 page not found\n", w.Body.String())
		})
	}
}

func TestUnmatchedMCPRoute_RoutesRegisteredAfterAttachStillServe(t *testing.T) {
	t.Parallel()

	mux := newUnmatchedRouteMux(t)

	req := httptest.NewRequest(http.MethodGet, "/registered-after-attach", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/mcp/bad-slug/.well-known/oauth-protected-resource", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
}
