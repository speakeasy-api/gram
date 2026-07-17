// Package interceptors holds per-vendor policies applied to remote-MCP proxy
// traffic. Each policy no-ops for upstreams it does not apply to.
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

// Case-insensitive User-Agent substrings for the clients in Figma's MCP
// catalog as of 2026-07-15. Tokens after the gap are inferred from product
// names, not yet observed in Gram traffic.
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

// figma rejects requests whose User-Agent is not a client in Figma's MCP
// catalog, since Figma only supports those clients and proxied traffic reaches
// it with Gram's transport. Policy gate, not a security boundary: User-Agent
// is spoofable.
type figma struct {
	figmaUpstream bool
	logger        *slog.Logger
}

// NewFigma builds the Figma client-allowlist policy for the given upstream.
func NewFigma(upstreamURL string, logger *slog.Logger) proxy.UserRequestInterceptor {
	figmaUpstream := false
	if u, err := url.Parse(upstreamURL); err == nil {
		figmaUpstream = strings.EqualFold(u.Hostname(), figmaMCPHost)
	}
	return &figma{figmaUpstream: figmaUpstream, logger: logger}
}

var _ proxy.UserRequestInterceptor = (*figma)(nil)

// Name implements [proxy.UserRequestInterceptor].
func (f *figma) Name() string { return "figma-user-agent-allowlist" }

// InterceptUserRequest implements [proxy.UserRequestInterceptor]. A missing
// User-Agent is rejected like an unlisted one.
func (f *figma) InterceptUserRequest(ctx context.Context, req *proxy.UserRequest) error {
	if !f.figmaUpstream {
		return nil
	}

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
