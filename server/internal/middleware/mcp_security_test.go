package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	gramOrigin     = "https://app.getgram.ai"
	hostileOrigin  = "https://evil.example.com"
	elementsOrigin = "https://docs.customer.com"
)

func newMCPSecurity(t *testing.T) func(http.Handler) http.Handler {
	t.Helper()

	mw, err := MCPSecurity(
		testenv.NewLogger(t),
		[]string{gramOrigin, "https://localhost:5173"},
	)
	require.NoError(t, err)
	return mw
}

// serveMCPSecurity runs a request through the middleware and reports whether
// the downstream handler was reached, alongside the recorded response.
func serveMCPSecurity(t *testing.T, req *http.Request) (*httptest.ResponseRecorder, bool) {
	t.Helper()

	reached := false
	handler := newMCPSecurity(t)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, reached
}

func mcpPost(path string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "https://app.getgram.ai"+path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// A native MCP client (Claude Desktop, Cursor, a CLI) sends neither
// Sec-Fetch-Site nor Origin. The stdlib fails open for those, which is what
// makes this protection deployable without per-server origin configuration.
func TestMCPSecurity_AllowsNonBrowserClient(t *testing.T) {
	t.Parallel()

	_, reached := serveMCPSecurity(t, mcpPost("/mcp/petstore"))

	require.True(t, reached, "a request with no browser fetch metadata must pass")
}

func TestMCPSecurity_AllowsSameOrigin(t *testing.T) {
	t.Parallel()

	req := mcpPost("/mcp/petstore")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", gramOrigin)

	_, reached := serveMCPSecurity(t, req)

	require.True(t, reached)
}

// The exposure this issue exists to close: a hostile page driving a
// credential-free public MCP server from a victim's browser.
func TestMCPSecurity_RejectsCrossSitePost(t *testing.T) {
	t.Parallel()

	req := mcpPost("/mcp/petstore")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", hostileOrigin)

	rec, reached := serveMCPSecurity(t, req)

	require.False(t, reached, "handler must not run for a cross-site request")
	require.Equal(t, http.StatusForbidden, rec.Code, "the MCP spec requires 403 for an invalid Origin")
}

// same-site is not same-origin: the stdlib rejects it, which is why local dev
// (dashboard and server on different ports of localhost) needs the site URL in
// the trusted set.
func TestMCPSecurity_RejectsSameSiteFromUntrustedOrigin(t *testing.T) {
	t.Parallel()

	req := mcpPost("/mcp/petstore")
	req.Header.Set("Sec-Fetch-Site", "same-site")
	req.Header.Set("Origin", "https://other.getgram.ai")

	_, reached := serveMCPSecurity(t, req)

	require.False(t, reached)
}

// The dashboard's MCP inspection tabs connect to a customer's custom domain,
// which is cross-site from the Gram origin and cannot be rebased onto the
// platform host because mcp_endpoint rows resolve by (slug, custom_domain_id).
func TestMCPSecurity_AllowsTrustedGramOriginCrossSite(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "https://mcp.customer.com/mcp/petstore", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", gramOrigin)

	_, reached := serveMCPSecurity(t, req)

	require.True(t, reached, "the Gram first-party origin must reach a custom domain")
}

// Elements is embedded on customer domains and is genuinely cross-site. Its
// exemption comes from the chat-session audience claim, proven upstream in
// chatSessionsCORS and carried on the request context.
func TestMCPSecurity_AllowsChatSessionTrustedOrigin(t *testing.T) {
	t.Parallel()

	req := mcpPost("/mcp/petstore")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", elementsOrigin)
	req = markChatSessionOriginTrusted(req)

	_, reached := serveMCPSecurity(t, req)

	require.True(t, reached, "an audience-validated Elements request must pass")
}

// DELETE terminates an MCP session and, for a proxy-backed server, tears down
// the real upstream session. It is not a safe method, so it is covered.
func TestMCPSecurity_RejectsCrossSiteDelete(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodDelete, "https://app.getgram.ai/mcp/petstore", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", hostileOrigin)

	rec, reached := serveMCPSecurity(t, req)

	require.False(t, reached)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

// GET opens the Streamable HTTP SSE stream. The stdlib treats safe methods as
// always allowed, so this is knowingly not covered — documented here so the
// gap is visible rather than assumed closed.
func TestMCPSecurity_AllowsCrossSiteGetAsSafeMethod(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "https://app.getgram.ai/mcp/petstore", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", hostileOrigin)

	_, reached := serveMCPSecurity(t, req)

	require.True(t, reached)
}

// Protection is attached to the shared route, not to a backend. /mcp/{slug}
// fronts both toolset-backed and meta-MCP-backed servers, so a later backend
// addition cannot silently regress out of coverage.
func TestMCPSecurity_CoversEveryMCPJSONRPCRoute(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/mcp/petstore",              // toolset-backed and meta-MCP-backed
		"/x/mcp/petstore",            // experimental runtime
		"/platform/mcp/gram-billing", // platform toolsets
		"/platform-mcp",              // Gram's own platform MCP server
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			req := mcpPost(path)
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			req.Header.Set("Origin", hostileOrigin)

			rec, reached := serveMCPSecurity(t, req)

			require.False(t, reached)
			require.Equal(t, http.StatusForbidden, rec.Code)
		})
	}
}

// The OAuth, metadata, and install routes share the /mcp/ prefix but are not
// JSON-RPC endpoints; they have their own browser-facing flows.
func TestMCPSecurity_IgnoresNonJSONRPCRoutes(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/mcp/petstore/token",
		"/mcp/petstore/register",
		"/mcp/petstore/connect",
		"/platform-mcp/authorize",
		"/platform-mcp/token",
		"/platform-mcp/provider-setup",
		"/rpc/chatSessions.create",
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			req := mcpPost(path)
			req.Header.Set("Sec-Fetch-Site", "cross-site")
			req.Header.Set("Origin", hostileOrigin)

			_, reached := serveMCPSecurity(t, req)

			require.True(t, reached, "non-JSON-RPC routes must be untouched")
		})
	}
}

func TestMCPSecurity_ContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		wantStatus  int
	}{
		// The CORS-simple set: these reach a handler without a preflight, so
		// they are how the Origin check would be routed around.
		{name: "text/plain", contentType: "text/plain", wantStatus: http.StatusUnsupportedMediaType},
		{name: "text/plain with charset", contentType: "text/plain;charset=UTF-8", wantStatus: http.StatusUnsupportedMediaType},
		{name: "form urlencoded", contentType: "application/x-www-form-urlencoded", wantStatus: http.StatusUnsupportedMediaType},
		{name: "multipart", contentType: "multipart/form-data; boundary=x", wantStatus: http.StatusUnsupportedMediaType},

		// Fetch safelists these without a preflight, but mime.ParseMediaType
		// rejects them. Falling back to the bare type token keeps them closed.
		{name: "text/plain empty params", contentType: "text/plain;;", wantStatus: http.StatusUnsupportedMediaType},
		{name: "text/plain bare param", contentType: "text/plain; x", wantStatus: http.StatusUnsupportedMediaType},
		{name: "text/plain duplicate charset", contentType: "text/plain; charset=utf-8; charset=iso-8859-1", wantStatus: http.StatusUnsupportedMediaType},
		{name: "multipart empty boundary", contentType: "multipart/form-data; boundary=", wantStatus: http.StatusUnsupportedMediaType},
		{name: "uppercase text/plain", contentType: "TEXT/PLAIN", wantStatus: http.StatusUnsupportedMediaType},

		// Everything else passes. Requiring application/json outright would
		// reject conforming non-browser clients for no security gain.
		{name: "json", contentType: "application/json", wantStatus: http.StatusOK},
		{name: "json with charset", contentType: "application/json; charset=utf-8", wantStatus: http.StatusOK},
		{name: "absent", contentType: "", wantStatus: http.StatusOK},
		{name: "unparseable non-safelisted", contentType: "application/json;;", wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "https://app.getgram.ai/mcp/petstore", strings.NewReader("{}"))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}

			rec, _ := serveMCPSecurity(t, req)

			require.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

// A GET carries no body, so Content-Type is not consulted for it.
func TestMCPSecurity_ContentTypeOnlyCheckedForPost(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "https://app.getgram.ai/mcp/petstore", nil)
	req.Header.Set("Content-Type", "text/plain")

	_, reached := serveMCPSecurity(t, req)

	require.True(t, reached)
}

func TestMCPSecurity_RejectsInvalidTrustedOrigin(t *testing.T) {
	t.Parallel()

	_, err := MCPSecurity(testenv.NewLogger(t), []string{"app.getgram.ai"})

	require.Error(t, err, "an origin without a scheme must fail at construction, not silently at runtime")
}

// server-url and site-url are ordinary URL flags elsewhere and may carry a
// trailing slash; AddTrustedOrigin rejects any path, so an unnormalized value
// would refuse to boot the server.
func TestMCPSecurity_NormalizesTrustedOrigins(t *testing.T) {
	t.Parallel()

	mw, err := MCPSecurity(testenv.NewLogger(t), []string{"https://app.getgram.ai/"})
	require.NoError(t, err)

	reached := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "https://mcp.customer.com/mcp/petstore", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", gramOrigin)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.True(t, reached, "a trailing slash in the flag must still yield a usable trusted origin")
}

func TestMCPSecurity_SkipsEmptyTrustedOrigins(t *testing.T) {
	t.Parallel()

	_, err := MCPSecurity(testenv.NewLogger(t), []string{"", gramOrigin})

	require.NoError(t, err, "an unset server-url or site-url flag must not break startup")
}
