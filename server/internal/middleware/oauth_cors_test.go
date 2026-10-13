package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCORSMiddleware_SharedOAuthPublicEndpoints(t *testing.T) {
	t.Parallel()

	for _, env := range []string{"local", "dev", "prod"} {
		for _, tc := range []struct {
			path   string
			method string
		}{
			{"/.well-known/oauth-authorization-server/oauth/usi/issuer", http.MethodGet},
			{"/oauth/usi/issuer/.well-known/oauth-authorization-server", http.MethodGet},
			{"/oauth/usi/issuer/token", http.MethodPost},
			{"/oauth/usi/issuer/register", http.MethodPost},
			{"/oauth/usi/issuer/revoke", http.MethodPost},
		} {
			for _, method := range []string{tc.method, http.MethodOptions} {
				t.Run(env+"/"+method+tc.path, func(t *testing.T) {
					t.Parallel()

					reached := false
					handler := CORSMiddleware(env, "https://platform.example", []string{"https://other-platform.example"}, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						reached = true
						// OAuth errors must be readable too.
						w.WriteHeader(http.StatusBadRequest)
					}))
					req := httptest.NewRequest(method, tc.path, nil)
					req.Header.Set("Origin", "https://client.example")
					req.Header.Set("Access-Control-Request-Method", tc.method)
					req.Header.Set("Access-Control-Request-Headers", "content-type, authorization")
					w := httptest.NewRecorder()
					w.Header().Add("Vary", "Accept-Encoding")
					handler.ServeHTTP(w, req)

					require.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
					require.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
					require.Equal(t, tc.method, w.Header().Get("Access-Control-Allow-Methods"))
					require.Equal(t, "Content-Type, Authorization", w.Header().Get("Access-Control-Allow-Headers"))
					require.Equal(t, "Accept-Encoding", w.Header().Get("Vary"))
					if method == http.MethodOptions {
						require.Equal(t, http.StatusNoContent, w.Code)
						require.False(t, reached)
					} else {
						require.Equal(t, http.StatusBadRequest, w.Code)
						require.True(t, reached)
					}
				})
			}
		}
	}
}

func TestCORSMiddleware_SharedOAuthPrivateRoutes(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/oauth/usi/issuer/authorize",
		"/oauth/usi/issuer/connect",
		"/oauth/usi/issuer/connect/remote-session",
		"/oauth/usi/issuer/connect/mcp",
		"/oauth/usi/issuer/future-endpoint",
		"/oauth/usi/issuer/token/extra",
		"/oauth/usi/issuer/token/",
		"/oauth/usi/issuer/tokenize",
		"/oauth/usi//token",
		"/oauth/usi/issuer",
		"/oauth/usi/",
		"/.well-known/oauth-authorization-server/oauth/usi/",
		"/.well-known/oauth-authorization-server/oauth/usi/issuer/extra",
		"/oauth/usi/issuer/.well-known/oauth-authorization-server/extra",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodOptions, http.MethodDelete} {
			t.Run(method+path, func(t *testing.T) {
				t.Parallel()

				reached := false
				handler := CORSMiddleware("prod", "https://platform.example", nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					reached = true
					w.WriteHeader(http.StatusOK)
				}))
				req := httptest.NewRequest(method, path, nil)
				req.Header.Set("Origin", "https://client.example")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)

				for name := range w.Header() {
					require.False(t, strings.HasPrefix(name, "Access-Control-"), "unexpected CORS header %s", name)
				}
				require.Equal(t, method != http.MethodOptions, reached)
			})
		}
	}
}

func TestCORSMiddleware_SharedOAuthDoesNotEchoRequestedHeaders(t *testing.T) {
	t.Parallel()

	handler := CORSMiddleware("prod", "https://platform.example", nil, nil)(http.NotFoundHandler())
	req := httptest.NewRequest(http.MethodOptions, "/oauth/usi/issuer/token", nil)
	req.Header.Set("Origin", "https://client.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodDelete)
	req.Header.Set("Access-Control-Request-Headers", "X-Arbitrary-Header")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// The browser cannot approve a DELETE or the unlisted request header.
	require.Equal(t, http.MethodPost, w.Header().Get("Access-Control-Allow-Methods"))
	require.Equal(t, "Content-Type, Authorization", w.Header().Get("Access-Control-Allow-Headers"))
}

func TestCORSMiddleware_SharedOAuthLeavesOtherRoutesUnchanged(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/oauth/usi-other/issuer/token",
		"/oauth/other/token",
		"/rpc/example",
		"/mcp/example/token",
		"/x/mcp/example/token",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			handler := CORSMiddleware("prod", "https://platform.example", nil, nil)(http.NotFoundHandler())
			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.Header.Set("Origin", "https://client.example")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			require.Equal(t, "https://platform.example", w.Header().Get("Access-Control-Allow-Origin"))
			require.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
		})
	}
}
