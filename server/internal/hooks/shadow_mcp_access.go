package hooks

import (
	"context"
	"errors"
	"net/url"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func (s *Service) enforceShadowMCPToolAccess(
	ctx context.Context,
	organizationID string,
	projectID string,
	userID string,
	policyID string,
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
	target := shadowMCPPolicyBypassTarget(evidence, toolName)
	if s.canBypassRiskPolicy(ctx, organizationID, userID, policyID, target) {
		s.logger.InfoContext(ctx, "shadow-mcp call allowed via risk policy bypass grant",
			attr.SlogEvent("shadow_mcp_policy_bypass_allow"),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProjectID(projectID),
			attr.SlogRiskPolicyID(policyID),
			attr.SlogValueAny(map[string]any{
				"target_kind": target.Kind,
				"target_key":  target.Key,
				"tool_name":   toolName,
			}),
		)
		return "", false
	}
	return detail, true
}

func shadowMCPPolicyBypassTarget(evidence shadowmcp.AccessEvidence, toolName string) *risk.PolicyBypassTarget {
	normalized := shadowmcp.NormalizeAccessEvidence(evidence)
	if serverURL := normalizedShadowMCPServerURL(normalized.FullURL); serverURL != "" {
		label := serverURL
		if observed := observedShadowMCPName(normalized, toolName); observed != nil && *observed != "" {
			label = *observed
		}
		target := risk.ShadowMCPServerPolicyBypassTarget(serverURL, label)
		return &target
	}

	return nil
}

func normalizedShadowMCPServerURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

func (s *Service) canBypassRiskPolicy(ctx context.Context, organizationID string, userID string, policyID string, target *risk.PolicyBypassTarget) bool {
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(policyID) == "" {
		return false
	}
	if target == nil {
		return false
	}
	serverURL := target.Dimensions[authz.SelectorKeyServerURL]
	if strings.TrimSpace(serverURL) == "" {
		return false
	}

	principals, err := authz.ResolveUserPrincipals(ctx, s.db, organizationID, userID)
	if err != nil {
		if !errors.Is(err, authz.ErrPrincipalNotFound) {
			s.logger.WarnContext(ctx, "failed to resolve principals for risk policy bypass",
				attr.SlogError(err),
				attr.SlogOrganizationID(organizationID),
				attr.SlogUserID(userID),
				attr.SlogRiskPolicyID(policyID),
			)
		}
		return false
	}
	grants, err := authz.LoadGrants(ctx, s.db, organizationID, principals)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to load risk policy bypass grants",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
			attr.SlogUserID(userID),
			attr.SlogRiskPolicyID(policyID),
		)
		return false
	}

	err = s.authz.EvaluateLoadedGrants(ctx, grants, authz.RiskPolicyBypassCheck(policyID, authz.RiskPolicyBypassDimensions{
		ServerURL: serverURL,
	}))
	return err == nil
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
