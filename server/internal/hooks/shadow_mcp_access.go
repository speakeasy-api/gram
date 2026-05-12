package hooks

import (
	"context"
	"fmt"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

type shadowMCPAccessRuleAuthorizer struct {
	engine *authz.Engine
}

func (a shadowMCPAccessRuleAuthorizer) RequireShadowMCPConnect(ctx context.Context, organizationID string, userID string, ruleID string, projectID string) error {
	if a.engine == nil {
		return fmt.Errorf("shadow mcp access rule authorizer unavailable: %w", authz.ErrMissingGrants)
	}
	if err := a.engine.RequirePrincipal(ctx, organizationID, userID, authz.ShadowMCPConnectCheck(ruleID, projectID)); err != nil {
		return fmt.Errorf("require shadow mcp connect: %w", err)
	}
	return nil
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

	decision := s.shadowMCPClient.EvaluateAccessRules(ctx, shadowMCPAccessRuleAuthorizer{engine: s.authz}, organizationID, projectID, userID, evidence)
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
	if decision.Allows() {
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
