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

func isFigmaUpstream(upstreamURL string) bool {
	u, err := url.Parse(upstreamURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Hostname(), figmaMCPHost)
}

// UserAgentAllowlistInterceptor rejects inbound MCP requests whose User-Agent
// does not match any allowed client token. Upstreams like Figma's only support
// a fixed catalog of MCP clients, and the proxy replaces the client's
// transport with its own — enforcing the allowlist here preserves the
// upstream's client policy for proxied traffic. It is a policy gate, not a
// security boundary: User-Agent is client-asserted and trivially spoofable.
type UserAgentAllowlistInterceptor struct {
	allowed    []string
	catalogURL string
	logger     *slog.Logger
}

var _ proxy.UserRequestInterceptor = (*UserAgentAllowlistInterceptor)(nil)

// NewUserAgentAllowlistInterceptor constructs the interceptor. allowed holds
// lowercase substrings matched case-insensitively against the User-Agent
// header; catalogURL is surfaced in rejection messages.
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
// User-Agent is rejected like an unlisted one: anonymous callers cannot be
// one of the allowed clients.
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
