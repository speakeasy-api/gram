package mcp_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/netingress"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
)

// runtimeInitializeResponse captures the initialize fields asserted by route tests.
type runtimeInitializeResponse struct {
	// ID is the JSON-RPC request identifier echoed by the server.
	ID int `json:"id"`

	// Result contains the negotiated protocol version.
	Result struct {
		// ProtocolVersion is the MCP revision chosen for the session.
		ProtocolVersion string `json:"protocolVersion"`
	} `json:"result"`
}

func TestRuntimeRoutes_TrailingSlash(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		private bool
		suffix  string
	}{
		{name: "public", suffix: ""},
		{name: "public slash", suffix: "/"},
		{name: "private", private: true, suffix: ""},
		{name: "private slash", private: true, suffix: "/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			toolset := createPublicMCPToolset(t, ctx, ti, authCtx, "runtime-route")

			mux := goahttp.NewMuxer()
			if tc.private {
				mux.Use(netingress.RouteGuard)
				mcp.AttachPrivate(mux, ti.service.Service, nil)
			} else {
				mcp.Attach(mux, ti.service.Service, nil)
			}

			path := "/mcp/" + toolset.McpSlug.String + tc.suffix + "?tag=one"
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(makeInitializeBody())).WithContext(ctx)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
			require.Empty(t, rr.Header().Get("Location"))
			require.NotEmpty(t, rr.Header().Get("Mcp-Session-Id"))

			// A successful initialize with the original ID proves the POST body
			// reached the runtime rather than a redirect or a router fallback.
			var response runtimeInitializeResponse
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
			require.Equal(t, 1, response.ID)
			require.Equal(t, mcpversions.Version20250326, response.Result.ProtocolVersion)

			for _, method := range []string{http.MethodGet, http.MethodDelete} {
				req = httptest.NewRequest(method, path, nil).WithContext(ctx)
				req.Header.Set("Accept", "text/event-stream")
				rr = httptest.NewRecorder()
				mux.ServeHTTP(rr, req)
				require.Equal(t, http.StatusMethodNotAllowed, rr.Code, rr.Body.String())
				require.Empty(t, rr.Header().Get("Location"))
				if method == http.MethodGet {
					require.Contains(t, rr.Body.String(), "compatibility probe")
				} else {
					require.Contains(t, rr.Body.String(), "session termination is not supported")
				}
			}

			for _, otherPath := range []string{
				"/mcp/runtime-route/token/",
				"/mcp/runtime-route/install/",
				"/.well-known/oauth-protected-resource/mcp/runtime-route/",
				"/.well-known/oauth-authorization-server/mcp/runtime-route/",
				"/agent-mcp/agent/",
				"/platform/mcp/server/",
				"/platform-mcp/",
				"/mcp/runtime-route//",
				"/mcp//",
			} {
				for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
					req = httptest.NewRequest(method, otherPath, nil).WithContext(ctx)
					rr = httptest.NewRecorder()
					mux.ServeHTTP(rr, req)
					require.Equal(t, http.StatusNotFound, rr.Code, "%s %s: %s", method, otherPath, rr.Body.String())
					require.Empty(t, rr.Header().Get("Location"))
				}
			}
		})
	}
}

func TestRuntimeRoutes_ProxyTrailingSlash(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		private bool
		suffix  string
	}{
		{name: "public"},
		{name: "public slash", suffix: "/"},
		{name: "private", private: true},
		{name: "private slash", private: true, suffix: "/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx.ProjectID)

			type upstreamHit struct {
				method  string
				accept  string
				session string
			}
			hits := make(chan upstreamHit, 2)
			const event = "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{\"level\":\"info\",\"data\":\"hello\"}}\n\n"
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == wellknown.OAuthProtectedResourcePath {
					http.NotFound(w, r)
					return
				}
				hits <- upstreamHit{method: r.Method, accept: r.Header.Get("Accept"), session: r.Header.Get("Mcp-Session-Id")}
				if r.Method == http.MethodGet {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte(event))
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(upstream.Close)

			const slug = "proxy-route"
			issuerID := createUserSessionIssuer(t, ctx, ti.conn, *authCtx.ProjectID)
			server, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, upstream.URL, slug, "public", issuerID)
			token := mintIssuerBearerForEndpoint(t, ctx, ti, slug, server, authCtx.ActiveOrganizationID)
			mux := goahttp.NewMuxer()
			if tc.private {
				mux.Use(netingress.RouteGuard)
				mcp.AttachPrivate(mux, ti.service.Service, nil)
			} else {
				mcp.Attach(mux, ti.service.Service, nil)
			}

			for _, method := range []string{http.MethodGet, http.MethodDelete} {
				req := httptest.NewRequest(method, "/mcp/"+slug+tc.suffix, nil).WithContext(ctx)
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Accept", "text/event-stream")
				req.Header.Set("Mcp-Session-Id", "proxy-session")
				rr := httptest.NewRecorder()
				mux.ServeHTTP(rr, req)
				require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
				require.Empty(t, rr.Header().Get("Location"))

				select {
				case hit := <-hits:
					require.Equal(t, method, hit.method)
					require.Equal(t, "proxy-session", hit.session)
					if method == http.MethodGet {
						require.Contains(t, hit.accept, "text/event-stream")
						require.Equal(t, "text/event-stream", rr.Header().Get("Content-Type"))
						require.Equal(t, event, rr.Body.String())
					}
				default:
					t.Fatal("runtime request did not reach the upstream")
				}
			}
		})
	}
}

func TestRuntimeRoutes_CustomDomainTrailingSlash(t *testing.T) {
	t.Parallel()

	for _, suffix := range []string{"", "/"} {
		t.Run("suffix="+suffix, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx.ProjectID)
			toolset, domain := createPublicMCPToolsetWithCustomDomain(t, ctx, ti, authCtx, "domain-toolset", "custom.example.com")
			const slug = "domain-endpoint"
			createToolsetMcpEndpoint(t, ctx, ti.conn, *authCtx.ProjectID, toolset.ID, slug, "public", uuid.NullUUID{UUID: domain.ID, Valid: true}, uuid.Nil)
			mux := goahttp.NewMuxer()
			mcp.Attach(mux, ti.service.Service, nil)

			for _, tc := range []struct {
				name   string
				domain *customdomains.Context
				status int
			}{
				{name: "matching domain", domain: &customdomains.Context{OrganizationID: authCtx.ActiveOrganizationID, Domain: domain.Domain, DomainID: domain.ID}, status: http.StatusOK},
				{name: "platform", status: http.StatusNotFound},
				{name: "wrong domain", domain: &customdomains.Context{OrganizationID: authCtx.ActiveOrganizationID, Domain: "other.example.com", DomainID: uuid.New()}, status: http.StatusNotFound},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					requestCtx := ctx
					if tc.domain != nil {
						requestCtx = customdomains.WithContext(ctx, tc.domain)
					}
					req := httptest.NewRequest(http.MethodPost, "/mcp/"+slug+suffix, bytes.NewReader(makeInitializeBody())).WithContext(requestCtx)
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Accept", "application/json")
					rr := httptest.NewRecorder()
					mux.ServeHTTP(rr, req)
					require.Equal(t, tc.status, rr.Code, rr.Body.String())
					require.Empty(t, rr.Header().Get("Location"))
					if tc.status == http.StatusOK {
						var response runtimeInitializeResponse
						require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
						require.Equal(t, 1, response.ID)
						require.Equal(t, mcpversions.Version20250326, response.Result.ProtocolVersion)
					} else {
						require.Contains(t, rr.Body.String(), "mcp server not found")
					}
				})
			}
		})
	}
}
