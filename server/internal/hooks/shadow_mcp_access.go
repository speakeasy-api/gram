package hooks

import (
	"context"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

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

	decision := s.shadowMCPClient.EvaluateAccessRules(ctx, organizationID, projectID, evidence)
	s.logger.InfoContext(ctx, "evaluated shadow mcp access rules",
		attr.SlogEvent("shadow_mcp_access_rule_evaluated"),
		attr.SlogOrganizationID(organizationID),
		attr.SlogProjectID(projectID),
		attr.SlogValueAny(map[string]any{
			"outcome":                   decision.Outcome,
			"shadow_mcp_access_rule_id": decision.RuleID,
			"reason":                    decision.Reason,
			"tool_name":                 toolName,
		}),
	)
	switch decision.Outcome {
	case shadowmcp.AccessRuleOutcomeDenied, shadowmcp.AccessRuleOutcomeError:
		return decision.Reason, true
	case shadowmcp.AccessRuleOutcomeAllowed:
		return "", false
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
