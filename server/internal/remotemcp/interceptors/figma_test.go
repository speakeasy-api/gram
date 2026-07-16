package interceptors_test

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/interceptors"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const (
	figmaUpstreamURL = "https://mcp.figma.com/mcp"
	figmaCatalogURL  = "https://www.figma.com/mcp-catalog/"
)

func newUserRequestWithUserAgent(userAgent string) *proxy.UserRequest {
	r := httptest.NewRequest("POST", "/x/mcp/figma", nil)
	r.Header.Set("User-Agent", userAgent)
	return &proxy.UserRequest{
		UserHTTPRequest: r,
		JSONRPCMessages: nil,
	}
}

func TestFigma_AllowsCatalogClients(t *testing.T) {
	t.Parallel()

	policy := interceptors.NewFigma(figmaUpstreamURL, testenv.NewLogger(t))

	// Real User-Agent strings observed in production.
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
		err := policy.InterceptUserRequest(t.Context(), newUserRequestWithUserAgent(userAgent))
		require.NoError(t, err, "user agent %q should be allowed", userAgent)
	}
}

func TestFigma_MatchesCaseInsensitively(t *testing.T) {
	t.Parallel()

	policy := interceptors.NewFigma(figmaUpstreamURL, testenv.NewLogger(t))

	err := policy.InterceptUserRequest(t.Context(), newUserRequestWithUserAgent("CURSOR/3.9.16 (DARWIN ARM64)"))
	require.NoError(t, err)
}

func TestFigma_RejectsUnlistedClient(t *testing.T) {
	t.Parallel()

	policy := interceptors.NewFigma(figmaUpstreamURL, testenv.NewLogger(t))

	unlisted := []string{
		"python-httpx/0.28.1",
		"node",
		"Go-http-client/2.0",
		"curl/8.7.1",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36",
	}

	for _, userAgent := range unlisted {
		err := policy.InterceptUserRequest(t.Context(), newUserRequestWithUserAgent(userAgent))
		require.Error(t, err, "user agent %q should be rejected", userAgent)

		var reject *proxy.RejectError
		require.ErrorAs(t, err, &reject, "rejection should be a *proxy.RejectError")
		require.Equal(t, proxy.RejectCodeServerError, reject.Code)
		require.Contains(t, reject.Message, userAgent)
		require.Contains(t, reject.Message, figmaCatalogURL)
	}
}

func TestFigma_RejectsMissingUserAgent(t *testing.T) {
	t.Parallel()

	policy := interceptors.NewFigma(figmaUpstreamURL, testenv.NewLogger(t))

	err := policy.InterceptUserRequest(t.Context(), newUserRequestWithUserAgent(""))
	require.Error(t, err)

	var reject *proxy.RejectError
	require.ErrorAs(t, err, &reject, "rejection should be a *proxy.RejectError")
	require.Equal(t, proxy.RejectCodeServerError, reject.Code)
	require.Contains(t, reject.Message, "missing User-Agent header")
}

func TestFigma_RejectsNilHTTPRequest(t *testing.T) {
	t.Parallel()

	policy := interceptors.NewFigma(figmaUpstreamURL, testenv.NewLogger(t))

	err := policy.InterceptUserRequest(t.Context(), &proxy.UserRequest{})
	require.Error(t, err)

	var reject *proxy.RejectError
	require.ErrorAs(t, err, &reject, "rejection should be a *proxy.RejectError")
	require.Equal(t, proxy.RejectCodeServerError, reject.Code)
}

func TestFigma_AppliesToFigmaUpstreamVariants(t *testing.T) {
	t.Parallel()

	for _, upstream := range []string{
		figmaUpstreamURL,
		"https://MCP.FIGMA.COM/mcp",
		"https://mcp.figma.com:443/mcp",
	} {
		policy := interceptors.NewFigma(upstream, testenv.NewLogger(t))
		err := policy.InterceptUserRequest(t.Context(), newUserRequestWithUserAgent("python-httpx/0.28.1"))
		require.Error(t, err, "upstream %q should enforce the allowlist", upstream)
	}
}

func TestFigma_IgnoresNonFigmaUpstreams(t *testing.T) {
	t.Parallel()

	for _, upstream := range []string{
		"https://mcp.linear.app/mcp",
		"https://figma.com/mcp",
		"https://mcp.figma.com.evil.example/mcp",
		"://not-a-url",
		"",
	} {
		policy := interceptors.NewFigma(upstream, testenv.NewLogger(t))
		err := policy.InterceptUserRequest(t.Context(), newUserRequestWithUserAgent("python-httpx/0.28.1"))
		require.NoError(t, err, "upstream %q should not enforce the allowlist", upstream)
	}
}
