package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/channels"
)

func TestCORSMiddleware_PlatformOrigins(t *testing.T) {
	t.Parallel()

	const serverURL = "https://app.getgram.ai"
	platformOrigins := []string{"https://ai.speakeasy.com", serverURL}

	tests := []struct {
		name         string
		origin       string
		expected     string
		expectedVary string
	}{
		{name: "platform origin is echoed", origin: "https://ai.speakeasy.com", expected: "https://ai.speakeasy.com", expectedVary: "Origin"},
		{name: "server origin", origin: serverURL, expected: serverURL, expectedVary: "Origin"},
		{name: "unknown origin", origin: "https://evil.example", expected: serverURL, expectedVary: "Origin"},
		{name: "subdomain of a platform origin", origin: "https://dev.ai.speakeasy.com", expected: serverURL, expectedVary: "Origin"},
		{name: "no origin", origin: "", expected: serverURL, expectedVary: "Origin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := CORSMiddleware("prod", serverURL, platformOrigins, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, "https://mcp.customer.example/rpc/x", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			require.Equal(t, tt.expected, rec.Header().Get("Access-Control-Allow-Origin"))
			require.Equal(t, tt.expectedVary, rec.Header().Get("Vary"))
		})
	}
}

func TestCORSMiddleware_VaryOrigin(t *testing.T) {
	t.Parallel()

	const serverURL = "https://app.getgram.ai"
	serve := func(platformOrigins []string, path string) http.Header {
		handler := CORSMiddleware("prod", serverURL, platformOrigins, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodGet, "https://app.getgram.ai"+path, nil)
		req.Header.Set("Origin", "https://ai.speakeasy.com")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Header()
	}

	t.Run("no platform origins keeps today's headers", func(t *testing.T) {
		t.Parallel()
		h := serve(nil, "/rpc/x")
		require.Equal(t, serverURL, h.Get("Access-Control-Allow-Origin"))
		require.Empty(t, h.Get("Vary"))
	})

	t.Run("open well-known routes answer any origin without Vary", func(t *testing.T) {
		t.Parallel()
		h := serve([]string{"https://ai.speakeasy.com"}, "/.well-known/oauth-protected-resource/mcp/x")
		require.Equal(t, "*", h.Get("Access-Control-Allow-Origin"))
		require.Empty(t, h.Get("Vary"))
	})
}

// The dashboard on a platform origin reads the denial headers off a 403 to
// explain it; a browser only hands them over when they are exposed.
func TestCORSMiddleware_ExposesDenialHeaders(t *testing.T) {
	t.Parallel()

	const serverURL = "https://app.getgram.ai"
	handler := CORSMiddleware("prod", serverURL, []string{"https://ai.speakeasy.com"}, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	req := httptest.NewRequest(http.MethodGet, serverURL+"/rpc/telemetry.logs", nil)
	req.Header.Set("Origin", "https://ai.speakeasy.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	exposed := strings.Split(rec.Header().Get("Access-Control-Expose-Headers"), ", ")
	require.Contains(t, exposed, HeaderRequiredScope)
	require.Contains(t, exposed, HeaderRequiredResourceKind)
}

func TestCORSMiddleware_ChannelsMCPIsCredentialless(t *testing.T) {
	t.Parallel()

	for _, env := range []string{"local", "dev", "prod"} {
		for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden} {
			for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodOptions} {
				t.Run(env+"/"+method+"/"+http.StatusText(status), func(t *testing.T) {
					t.Parallel()
					reached := false
					handler := CORSMiddleware(env, "https://platform.example", []string{"https://other-platform.example"}, nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						reached = true
						if status == http.StatusUnauthorized {
							w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://platform.example/.well-known/oauth-protected-resource/channels/example-org/example-project/mcp"`)
						}
						w.WriteHeader(status)
					}))
					req := httptest.NewRequest(method, "/channels/example-org/example-project/mcp", nil)
					req.Header.Set("Origin", "https://client.example")
					req.Header.Set("Access-Control-Request-Method", http.MethodPost)
					req.Header.Set("Access-Control-Request-Headers", "content-type, authorization, mcp-protocol-version, mcp-method, mcp-name, mcp-session-id, last-event-id")
					w := httptest.NewRecorder()
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Add("Vary", "Accept-Encoding")
					handler.ServeHTTP(w, req)

					require.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
					require.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
					require.Equal(t, http.MethodPost, w.Header().Get("Access-Control-Allow-Methods"))
					allowed := strings.Split(w.Header().Get("Access-Control-Allow-Headers"), ", ")
					require.Equal(t, []string{"Accept", "Content-Type", "Authorization", "MCP-Protocol-Version", "Mcp-Method", "Mcp-Name", "Mcp-Session-Id", "Last-Event-ID"}, allowed)
					require.Contains(t, strings.Split(w.Header().Get("Access-Control-Expose-Headers"), ", "), "WWW-Authenticate")
					require.Equal(t, "Accept-Encoding", w.Header().Get("Vary"))
					if method == http.MethodOptions {
						require.Equal(t, http.StatusNoContent, w.Code)
						require.False(t, reached)
					} else {
						require.Equal(t, status, w.Code)
						require.True(t, reached)
						if status == http.StatusUnauthorized {
							require.Contains(t, w.Header().Get("WWW-Authenticate"), "resource_metadata=")
						}
					}
				})
			}
		}
	}
}

func TestCORSMiddleware_ChannelsMCPDoesNotEchoRequestedHeaders(t *testing.T) {
	t.Parallel()

	for _, env := range []string{"local", "dev", "prod"} {
		t.Run(env, func(t *testing.T) {
			t.Parallel()
			handler := CORSMiddleware(env, "https://platform.example", nil, nil)(http.NotFoundHandler())
			req := httptest.NewRequest(http.MethodOptions, "/channels/example-org/example-project/mcp", nil)
			req.Header.Set("Origin", "https://client.example")
			req.Header.Set("Access-Control-Request-Method", http.MethodDelete)
			req.Header.Set("Access-Control-Request-Headers", "Cookie, Gram-Key, X-Arbitrary-Header, Mcp-Param-Secret")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			require.Equal(t, http.MethodPost, w.Header().Get("Access-Control-Allow-Methods"))
			allowed := strings.ToLower(w.Header().Get("Access-Control-Allow-Headers"))
			for _, forbidden := range []string{"cookie", "gram-key", "x-arbitrary-header", "mcp-param-secret", "*"} {
				require.NotContains(t, allowed, forbidden)
			}
			require.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
		})
	}
}

func TestCORSMiddleware_ChannelsAPIPreservesPlatformPolicy(t *testing.T) {
	t.Parallel()

	for _, env := range []string{"local", "dev", "prod"} {
		for _, path := range []string{"/channels/example-org/example-project/api"} {
			for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodOptions} {
				t.Run(env+"/"+method+path, func(t *testing.T) {
					t.Parallel()
					handler := CORSMiddleware(env, "https://platform.example", nil, nil)(newMCPSecurity(t)(http.NotFoundHandler()))
					req := httptest.NewRequest(method, path, nil)
					req.Header.Set("Origin", "https://client.example")
					req.Header.Set("Sec-Fetch-Site", "cross-site")
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, req)

					wantOrigin := "https://platform.example"
					if env == "local" {
						wantOrigin = "https://client.example"
					}
					require.Equal(t, wantOrigin, w.Header().Get("Access-Control-Allow-Origin"))
					require.Equal(t, "true", w.Header().Get("Access-Control-Allow-Credentials"))
					if method == http.MethodOptions {
						require.Equal(t, http.StatusOK, w.Code)
					} else {
						require.Equal(t, http.StatusNotFound, w.Code, "browser API must not inherit MCP origin/content-type checks")
					}
					require.NotContains(t, w.Header().Get("Access-Control-Expose-Headers"), "WWW-Authenticate")
				})
			}
		}
	}
}

// Wildcard CORS makes responses readable; it does not admit browser origins.
// Preserve production ordering: CORS, then MCP security, then the resource.
func TestCORSMiddleware_ChannelsMCPPreservesOriginValidation(t *testing.T) {
	t.Parallel()

	const challenge = `Bearer resource_metadata="https://platform.example/.well-known/oauth-protected-resource/channels/example-org/example-project/mcp"`
	for _, env := range []string{"local", "dev", "prod"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			for _, tc := range []struct {
				name, origin string
				status       int
			}{
				{name: "unknown external origin", origin: "https://client.example", status: http.StatusForbidden},
				// newMCPSecurity explicitly configures this trusted origin.
				{name: "configured trusted origin", origin: "https://localhost:5173", status: http.StatusUnauthorized},
			} {
				t.Run(env+"/"+method+"/"+tc.name, func(t *testing.T) {
					t.Parallel()
					authCalls := 0
					resource := channels.New(nil, func(*http.Request) (channels.Principal, error) {
						authCalls++
						return channels.Principal{}, channels.ErrDenied
					})
					t.Cleanup(resource.Close)
					handler := CORSMiddleware(env, gramOrigin, nil, nil)(newMCPSecurity(t)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						// The route's challenge wrapper supplies this header when
						// the Channels handler rejects an unauthenticated request.
						w.Header().Set("WWW-Authenticate", challenge)
						resource.ServeHTTP(w, r)
					})))

					// A public preflight is not authorization to send the actual
					// MCP request from an untrusted browser origin.
					preflight := httptest.NewRequest(http.MethodOptions, gramOrigin+"/channels/example-org/example-project/mcp", nil)
					preflight.Header.Set("Origin", tc.origin)
					preflight.Header.Set("Access-Control-Request-Method", method)
					preflight.Header.Set("Access-Control-Request-Headers", "authorization, mcp-protocol-version")
					preflightResponse := httptest.NewRecorder()
					handler.ServeHTTP(preflightResponse, preflight)
					require.Equal(t, http.StatusNoContent, preflightResponse.Code)
					require.Equal(t, "*", preflightResponse.Header().Get("Access-Control-Allow-Origin"))
					require.Empty(t, preflightResponse.Header().Get("Access-Control-Allow-Credentials"))
					require.Zero(t, authCalls)

					req := httptest.NewRequest(method, gramOrigin+"/channels/example-org/example-project/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
					req.Header.Set("Origin", tc.origin)
					req.Header.Set("Sec-Fetch-Site", "cross-site")
					req.Header.Set("Content-Type", "application/json")
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, req)

					require.Equal(t, tc.status, w.Code)
					require.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
					require.Empty(t, w.Header().Get("Access-Control-Allow-Credentials"))
					require.Contains(t, strings.Split(w.Header().Get("Access-Control-Expose-Headers"), ", "), "WWW-Authenticate")
					if tc.status == http.StatusForbidden {
						require.Zero(t, authCalls, "CORS must not bypass MCP origin protection")
						require.Empty(t, w.Header().Get("WWW-Authenticate"))
						require.Contains(t, w.Body.String(), "cross-origin request rejected")
					} else {
						require.Equal(t, 1, authCalls, "a trusted origin must reach Channels authentication")
						require.Equal(t, challenge, w.Header().Get("WWW-Authenticate"))
						require.Contains(t, w.Body.String(), "authentication required")
					}
				})
			}
		}
	}
}
