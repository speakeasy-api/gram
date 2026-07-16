package interceptors

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

const figmaMCPHost = "mcp.figma.com"

const figmaMCPCatalogURL = "https://www.figma.com/mcp-catalog/"

// figmaAllowedUserAgents holds case-insensitive User-Agent substrings, one or
// more per agent in Figma's MCP client catalog (figmaMCPCatalogURL, as of
// 2026-07-15). The first group is verified against real client traffic on
// Gram-hosted MCP servers; the second group has not been observed yet, so the
// tokens are a best guess at the product identifier each User-Agent carries.
var figmaAllowedUserAgents = []string{
	"claude-user", // Claude via claude.ai remote connectors
	"claude-code/",
	"cursor/",
	"visual studio code/",
	"copilot/",      // GitHub Copilot CLI
	"codex_cli_rs/", // Codex CLI
	"openai-mcp/",   // ChatGPT and OpenAI Agent Builder
	"chatgpt-user/", // ChatGPT server-side fetches
	"zed/",
	"kiro/",
	"replit", // e.g. Replit-Agent-MCP-Client/1.0
	"notion-mcp-client/",

	"xcode",
	"grok",
	"antigravity",
	"slack",
	"windsurf",
	"warp/",
	"factory",
	"droid/", // Factory's droid CLI
	"augment",
	"rovo",
	"android studio",
	"androidstudio",
	"amazon-q",
	"amazonq",
	"openhands",
	"servicenow",
	"devin",
}

// figma rejects inbound MCP requests whose User-Agent does not match a client
// in Figma's MCP catalog. Figma's hosted MCP server only supports those
// clients, and the proxy replaces the client's transport with its own —
// enforcing the allowlist here preserves Figma's client policy for proxied
// traffic. It is a policy gate, not a security boundary: User-Agent is
// client-asserted and trivially spoofable.
type figma struct {
	logger *slog.Logger
}

// NewFigma builds the Figma client-allowlist policy.
func NewFigma(logger *slog.Logger) UpstreamPolicy {
	return &figma{logger: logger}
}

var _ UpstreamPolicy = (*figma)(nil)

// Name implements [proxy.UserRequestInterceptor].
func (f *figma) Name() string { return "figma-user-agent-allowlist" }

// Match implements [UpstreamPolicy]. Unparseable URLs are treated as
// non-Figma; the proxy rejects them downstream for its own reasons.
func (f *figma) Match(upstreamURL string) bool {
	u, err := url.Parse(upstreamURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), figmaMCPHost)
}

// InterceptUserRequest implements [proxy.UserRequestInterceptor]. A missing
// User-Agent is rejected like an unlisted one: anonymous callers cannot be
// one of the allowed clients.
func (f *figma) InterceptUserRequest(ctx context.Context, req *proxy.UserRequest) error {
	var userAgent string
	if req != nil && req.UserHTTPRequest != nil {
		userAgent = req.UserHTTPRequest.UserAgent()
	}

	loweredUA := strings.ToLower(userAgent)
	for _, token := range figmaAllowedUserAgents {
		if strings.Contains(loweredUA, token) {
			return nil
		}
	}

	f.logger.InfoContext(ctx, "rejected mcp request from unlisted client",
		attr.SlogHTTPRequestHeaderUserAgent(userAgent),
	)

	if userAgent == "" {
		return &proxy.RejectError{
			Code:    proxy.RejectCodeServerError,
			Message: fmt.Sprintf("missing User-Agent header: this MCP server only accepts requests from approved clients, see %s", figmaMCPCatalogURL),
			Data:    nil,
		}
	}

	return &proxy.RejectError{
		Code:    proxy.RejectCodeServerError,
		Message: fmt.Sprintf("client %q is not an approved client for this MCP server, see %s", conv.TruncateString(userAgent, 200), figmaMCPCatalogURL),
		Data:    nil,
	}
}
