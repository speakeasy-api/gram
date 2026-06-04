package hooks

import (
	"context"
	"net/url"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

// shadowServerURLHost is the stable per-server key for risk_policy:bypass
// narrowing: the URL host of a remote shadow MCP (e.g. "mcp.datadoghq.com").
// Falls back to URLHost, then to the tool-prefix ServerIdentity for local stdio
// servers that have no URL. The same value is used at block-emit and grant-check
// time so a server-narrowed bypass matches.
func shadowServerURLHost(ev shadowmcp.AccessEvidence) string {
	if ev.FullURL != "" {
		if u, err := url.Parse(ev.FullURL); err == nil && u.Host != "" {
			return u.Host
		}
	}
	if ev.URLHost != "" {
		return ev.URLHost
	}
	return ev.ServerIdentity
}

func (s *Service) enforceShadowMCPToolAccess(
	ctx context.Context,
	organizationID string,
	projectID string,
	userID string,
	toolInput any,
	toolName string,
	evidence shadowmcp.AccessEvidence,
) (string, bool) {
	if s.shadowMCPClient == nil {
		return "Shadow MCP validation is unavailable", true
	}

	detail, denied := s.shadowMCPClient.ValidateToolsetCall(ctx, toolInput, toolName, organizationID)
	if !denied {
		return "", false
	}
	return detail, true
}

func codexShadowMCPEvidence(payload *gen.CodexPayload) shadowmcp.AccessEvidence {
	return shadowmcp.AccessEvidence{
		FullURL:        "",
		URLHost:        "",
		ServerIdentity: mcpServerIdentityFromToolName(ptrString(payload.ToolName)),
	}
}

func cursorShadowMCPEvidence(payload *gen.CursorPayload) shadowmcp.AccessEvidence {
	return shadowmcp.AccessEvidence{
		FullURL:        ptrString(payload.URL),
		URLHost:        "",
		ServerIdentity: cursorMCPToolSource(payload),
	}
}

func claudeShadowMCPEvidence(rawToolName string) shadowmcp.AccessEvidence {
	return shadowmcp.AccessEvidence{
		FullURL:        "",
		URLHost:        "",
		ServerIdentity: mcpServerIdentityFromToolName(rawToolName),
	}
}

func mcpServerIdentityFromToolName(rawName string) string {
	if !strings.HasPrefix(rawName, "mcp__") {
		return ""
	}
	parts := strings.SplitN(rawName, "__", 3)
	if len(parts) != 3 {
		return ""
	}
	return parts[1]
}

func ptrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
