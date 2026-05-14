package hooks

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	accesssvc "github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

const shadowMCPApprovalRequestTokenTTL = 24 * time.Hour

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
	if decision.Allows() {
		return "", false
	}
	return s.withShadowMCPApprovalRequestLink(ctx, organizationID, projectID, userID, toolInput, toolName, detail, evidence), true
}

func (s *Service) withShadowMCPApprovalRequestLink(
	ctx context.Context,
	organizationID string,
	projectID string,
	userID string,
	toolInput any,
	toolName string,
	detail string,
	evidence shadowmcp.AccessEvidence,
) string {
	if s.siteURL == nil || strings.TrimSpace(s.jwtSecret) == "" {
		return detail
	}

	var toolCall *string
	if toolInput != nil {
		if raw, err := json.Marshal(toolInput); err == nil {
			value := string(raw)
			toolCall = &value
		} else {
			s.logger.WarnContext(ctx, "failed to marshal shadow mcp blocked tool input", attr.SlogError(err))
		}
	}

	token, _, err := accesssvc.GenerateShadowMCPApprovalRequestToken(s.jwtSecret, accesssvc.ShadowMCPApprovalRequestTokenInput{
		OrganizationID:         organizationID,
		ProjectID:              projectID,
		RequesterUserID:        userID,
		ObservedName:           nil,
		ObservedFullURL:        optionalString(evidence.FullURL),
		ObservedURLHost:        optionalString(evidence.URLHost),
		ObservedServerIdentity: optionalString(evidence.ServerIdentity),
		ToolName:               optionalString(toolName),
		ToolCall:               toolCall,
		BlockReason:            optionalString(detail),
		RiskPolicyID:           nil,
		RiskResultID:           nil,
	}, shadowMCPApprovalRequestTokenTTL)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to generate shadow mcp approval request token", attr.SlogError(err))
		return detail
	}

	requestURL := s.siteURL.JoinPath("shadow-mcp", "request")
	requestURL.Fragment = "request_token=" + token
	return detail + "\n\nRequest access:\n" + requestURL.String()
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

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}
