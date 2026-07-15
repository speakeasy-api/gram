package remotemcp

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newFigmaInterceptorForTest(t *testing.T) *UserAgentAllowlistInterceptor {
	t.Helper()
	return NewUserAgentAllowlistInterceptor(figmaAllowedUserAgents, figmaMCPCatalogURL, testenv.NewLogger(t))
}

func newUserRequestWithUserAgent(userAgent string) *proxy.UserRequest {
	r := httptest.NewRequest("POST", "/x/mcp/figma", nil)
	r.Header.Set("User-Agent", userAgent)
	return &proxy.UserRequest{
		UserHTTPRequest: r,
		JSONRPCMessages: nil,
	}
}

func TestUserAgentAllowlistInterceptor_AllowsCatalogClients(t *testing.T) {
	t.Parallel()

	interceptor := newFigmaInterceptorForTest(t)

	// Real User-Agent strings observed on Gram-hosted MCP servers in
	// production, one per catalog client family with a verified token.
	observed := []string{
		"Claude-User",
		"claude-code/2.1.210 (cli)",
		"claude-code/2.1.209 (claude-desktop, agent-sdk/0.3.209)",
		"Cursor/3.9.16 (darwin arm64)",
		"Visual Studio Code/1.128.0",
		"copilot/1.0.70 (darwin v24.16.0) term/Apple_Terminal",
		"codex_cli_rs/0.144.1 (Mac OS 26.4.0; arm64) iTerm.app/3.6.7",
		"openai-mcp/1.0.0 (ChatGPT)",
		"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko); compatible; ChatGPT-User/1.0; +https://openai.com/bot",
		"Zed/1.9.0+stable.316.ced90fc636c4ede05402befc38a63bae7fd741bd (macos; aarch64)",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Kiro/1.0.0 Chrome/142.0.7444.265 Electron/39.6.0 Safari/537.36",
		"Replit-Agent-MCP-Client/1.0",
		"Notion-MCP-Client/1.0",
	}

	for _, userAgent := range observed {
		err := interceptor.InterceptUserRequest(t.Context(), newUserRequestWithUserAgent(userAgent))
		require.NoError(t, err, "user agent %q should be allowed", userAgent)
	}
}

func TestUserAgentAllowlistInterceptor_MatchesCaseInsensitively(t *testing.T) {
	t.Parallel()

	interceptor := newFigmaInterceptorForTest(t)

	err := interceptor.InterceptUserRequest(t.Context(), newUserRequestWithUserAgent("CURSOR/3.9.16 (DARWIN ARM64)"))
	require.NoError(t, err)
}

func TestUserAgentAllowlistInterceptor_RejectsUnlistedClient(t *testing.T) {
	t.Parallel()

	interceptor := newFigmaInterceptorForTest(t)

	unlisted := []string{
		"python-httpx/0.28.1",
		"node",
		"Go-http-client/2.0",
		"curl/8.7.1",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36",
	}

	for _, userAgent := range unlisted {
		err := interceptor.InterceptUserRequest(t.Context(), newUserRequestWithUserAgent(userAgent))
		require.Error(t, err, "user agent %q should be rejected", userAgent)

		var reject *proxy.RejectError
		require.ErrorAs(t, err, &reject, "rejection should be a *proxy.RejectError")
		require.Equal(t, proxy.RejectCodeServerError, reject.Code)
		require.Contains(t, reject.Message, userAgent)
		require.Contains(t, reject.Message, figmaMCPCatalogURL)
	}
}

func TestUserAgentAllowlistInterceptor_RejectsMissingUserAgent(t *testing.T) {
	t.Parallel()

	interceptor := newFigmaInterceptorForTest(t)

	err := interceptor.InterceptUserRequest(t.Context(), newUserRequestWithUserAgent(""))
	require.Error(t, err)

	var reject *proxy.RejectError
	require.ErrorAs(t, err, &reject, "rejection should be a *proxy.RejectError")
	require.Equal(t, proxy.RejectCodeServerError, reject.Code)
	require.Contains(t, reject.Message, "missing User-Agent header")
}

func TestUserAgentAllowlistInterceptor_RejectsNilHTTPRequest(t *testing.T) {
	t.Parallel()

	interceptor := newFigmaInterceptorForTest(t)

	err := interceptor.InterceptUserRequest(t.Context(), &proxy.UserRequest{})
	require.Error(t, err)

	var reject *proxy.RejectError
	require.ErrorAs(t, err, &reject, "rejection should be a *proxy.RejectError")
	require.Equal(t, proxy.RejectCodeServerError, reject.Code)
}

func TestIsFigmaUpstream(t *testing.T) {
	t.Parallel()

	require.True(t, isFigmaUpstream("https://mcp.figma.com/mcp"))
	require.True(t, isFigmaUpstream("https://MCP.FIGMA.COM/mcp"))
	require.True(t, isFigmaUpstream("https://mcp.figma.com:443/mcp"))

	require.False(t, isFigmaUpstream("https://mcp.linear.app/mcp"))
	require.False(t, isFigmaUpstream("https://figma.com/mcp"))
	require.False(t, isFigmaUpstream("https://mcp.figma.com.evil.example/mcp"))
	require.False(t, isFigmaUpstream("://not-a-url"))
	require.False(t, isFigmaUpstream(""))
}
