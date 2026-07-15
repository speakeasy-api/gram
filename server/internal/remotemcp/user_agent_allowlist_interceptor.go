package remotemcp

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

// figmaMCPHost is the host of Figma's hosted remote MCP server
// (https://mcp.figma.com/mcp). Figma only supports the MCP clients listed in
// its client catalog, so proxies targeting this host enforce a User-Agent
// allowlist derived from that catalog.
const figmaMCPHost = "mcp.figma.com"

// figmaMCPCatalogURL is surfaced in rejection messages so blocked callers can
// see which clients are supported.
const figmaMCPCatalogURL = "https://www.figma.com/mcp-catalog/"

// figmaAllowedUserAgents holds case-insensitive User-Agent substrings, one or
// more per agent in Figma's MCP client catalog (https://www.figma.com/mcp-catalog/
// as of 2026-07-15: Claude, Claude Code, Codex, Cursor, Xcode, VS Code, Kiro,
// Grok, Google Antigravity, GitHub Copilot CLI, Notion, Slack, ChatGPT,
// Windsurf, Replit, Warp, Factory, Augment, Rovo Studio, Android Studio,
// Amazon Q, OpenHands, Zed, ServiceNow Build Agent, Devin).
//
// The first group is verified against real client traffic observed on
// Gram-hosted MCP servers. The second group is inferred from product names —
// those clients have not been observed in Gram traffic yet, so the tokens are
// a best guess at the product identifier their User-Agent would carry.
var figmaAllowedUserAgents = []string{
	"claude-user",         // Claude — claude.ai remote connectors
	"claude-code/",        // Claude Code — cli, sdk, desktop, vscode, and slack surfaces
	"cursor/",             // Cursor
	"visual studio code/", // VS Code
	"copilot/",            // GitHub Copilot CLI
	"codex_cli_rs/",       // Codex CLI
	"openai-mcp/",         // ChatGPT and OpenAI Agent Builder MCP client
	"chatgpt-user/",       // ChatGPT server-side fetches
	"zed/",                // Zed
	"kiro/",               // Kiro — Electron-style UA with an embedded Kiro/<version> token
	"replit",              // Replit — e.g. Replit-Agent-MCP-Client/1.0
	"notion-mcp-client/",  // Notion

	"xcode",          // Xcode (beta) — inferred
	"grok",           // Grok — inferred
	"antigravity",    // Google Antigravity — inferred
	"slack",          // Slack — inferred
	"windsurf",       // Windsurf — inferred
	"warp/",          // Warp — inferred
	"factory",        // Factory — inferred
	"droid/",         // Factory's droid CLI — inferred
	"augment",        // Augment — inferred
	"rovo",           // Rovo Studio — inferred
	"android studio", // Android Studio — inferred
	"androidstudio",  // Android Studio, no-space variant — inferred
	"amazon-q",       // Amazon Q — inferred
	"amazonq",        // Amazon Q, no-dash variant — inferred
	"openhands",      // OpenHands — inferred
	"servicenow",     // ServiceNow Build Agent — inferred
	"devin",          // Devin — inferred
}

// isFigmaUpstream reports whether upstreamURL points at Figma's hosted MCP
// server. Unparseable URLs are treated as non-Figma; the proxy rejects them
// downstream for its own reasons.
func isFigmaUpstream(upstreamURL string) bool {
	u, err := url.Parse(upstreamURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), figmaMCPHost)
}

// UserAgentAllowlistInterceptor rejects inbound MCP requests whose User-Agent
// header does not match any allowed client pattern. It is a
// [proxy.UserRequestInterceptor], so it runs on every POSTed JSON-RPC message
// (including initialize) before the request is forwarded upstream. Rejections
// surface to the caller as a JSON-RPC error envelope.
//
// This is a policy gate, not a security boundary: User-Agent is
// client-asserted and trivially spoofable. Upstream servers such as Figma's
// only support a fixed catalog of MCP clients, and the proxy replaces the
// client's transport with its own — enforcing the allowlist here preserves
// the upstream's client policy for proxied traffic.
type UserAgentAllowlistInterceptor struct {
	allowed    []string
	catalogURL string
	logger     *slog.Logger
}

var _ proxy.UserRequestInterceptor = (*UserAgentAllowlistInterceptor)(nil)

// NewUserAgentAllowlistInterceptor constructs the interceptor. allowed holds
// lowercase substrings matched case-insensitively against the inbound
// User-Agent header; catalogURL is included in the rejection message so
// blocked callers can discover which clients are supported.
func NewUserAgentAllowlistInterceptor(allowed []string, catalogURL string, logger *slog.Logger) *UserAgentAllowlistInterceptor {
	return &UserAgentAllowlistInterceptor{
		allowed:    allowed,
		catalogURL: catalogURL,
		logger:     logger,
	}
}

// Name implements [proxy.UserRequestInterceptor].
func (i *UserAgentAllowlistInterceptor) Name() string {
	return "user-agent-allowlist"
}

// InterceptUserRequest implements [proxy.UserRequestInterceptor]. A missing
// User-Agent header is rejected like an unlisted one: the allowlist encodes
// "only these clients", and anonymous callers cannot be one of them.
func (i *UserAgentAllowlistInterceptor) InterceptUserRequest(ctx context.Context, req *proxy.UserRequest) error {
	var userAgent string
	if req != nil && req.UserHTTPRequest != nil {
		userAgent = req.UserHTTPRequest.UserAgent()
	}

	loweredUA := strings.ToLower(userAgent)
	for _, token := range i.allowed {
		if strings.Contains(loweredUA, token) {
			return nil
		}
	}

	i.logger.InfoContext(ctx, "rejected mcp request from unlisted client",
		attr.SlogHTTPRequestHeaderUserAgent(userAgent),
	)

	if userAgent == "" {
		return &proxy.RejectError{
			Code:    proxy.RejectCodeServerError,
			Message: fmt.Sprintf("missing User-Agent header: this MCP server only accepts requests from approved clients, see %s", i.catalogURL),
			Data:    nil,
		}
	}

	return &proxy.RejectError{
		Code:    proxy.RejectCodeServerError,
		Message: fmt.Sprintf("client %q is not an approved client for this MCP server, see %s", conv.TruncateString(userAgent, 200), i.catalogURL),
		Data:    nil,
	}
}
