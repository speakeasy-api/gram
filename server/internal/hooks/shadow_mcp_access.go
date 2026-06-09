package hooks

import (
	"context"
	"errors"
	"net/url"
	"strings"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
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
	if s.canBypassLegacyShadowMCPAccess(ctx, organizationID, projectID, userID, policyID, evidence) {
		s.logger.InfoContext(ctx, "shadow-mcp call allowed via legacy shadow mcp access rule",
			attr.SlogEvent("shadow_mcp_legacy_access_allow"),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProjectID(projectID),
			attr.SlogRiskPolicyID(policyID),
			attr.SlogValueAny(map[string]any{
				"tool_name": toolName,
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

func (s *Service) canBypassLegacyShadowMCPAccess(ctx context.Context, organizationID string, projectID string, userID string, policyID string, evidence shadowmcp.AccessEvidence) bool {
	if s.accessStore == nil || strings.TrimSpace(organizationID) == "" || strings.TrimSpace(projectID) == "" || strings.TrimSpace(policyID) == "" {
		return false
	}

	normalized := shadowmcp.NormalizeAccessEvidence(evidence)
	matchKinds, matchValues := legacyShadowMCPAccessRuleMatchCandidates(normalized)
	if len(matchKinds) == 0 {
		return false
	}

	rules, err := s.accessStore.ListMatchingRules(ctx, accesscontrol.MatchingRuleFilters{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ResourceType:   accesscontrol.ResourceTypeShadowMCP,
		MatchKinds:     matchKinds,
		MatchValues:    matchValues,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to list legacy shadow mcp access rules for risk policy bypass",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProjectID(projectID),
			attr.SlogRiskPolicyID(policyID),
		)
		return false
	}

	for _, rule := range rules {
		if rule.Disposition == accesscontrol.DispositionDenied && legacyShadowMCPAccessRuleAppliesToPolicy(rule, policyID) {
			return false
		}
	}

	for _, rule := range rules {
		if rule.Disposition != accesscontrol.DispositionAllowed || !legacyShadowMCPAccessRuleAppliesToPolicy(rule, policyID) {
			continue
		}
		if s.legacyShadowMCPAccessRuleAllowsUser(ctx, organizationID, userID, rule) {
			return true
		}
	}

	return false
}

func (s *Service) legacyShadowMCPAccessRuleAllowsUser(ctx context.Context, organizationID string, userID string, rule accesscontrol.AccessRule) bool {
	if strings.TrimSpace(rule.SourceRequestID) == "" {
		return true
	}
	if strings.TrimSpace(userID) == "" {
		return false
	}

	request, err := s.accessStore.GetRequest(ctx, organizationID, accesscontrol.ResourceTypeShadowMCP, rule.SourceRequestID)
	if err != nil {
		if !errors.Is(err, accesscontrol.ErrNotFound) {
			s.logger.WarnContext(ctx, "failed to load legacy shadow mcp source request for risk policy bypass",
				attr.SlogError(err),
				attr.SlogOrganizationID(organizationID),
				attr.SlogValueAny(map[string]any{
					"source_request_id": rule.SourceRequestID,
					"rule_id":           rule.ID,
				}),
			)
		}
		return false
	}

	return request.RequesterUserID == userID
}

func legacyShadowMCPAccessRuleMatchCandidates(evidence shadowmcp.AccessEvidence) ([]string, []string) {
	kinds := make([]string, 0, 3)
	values := make([]string, 0, 3)
	if evidence.FullURL != "" {
		kinds = append(kinds, accesscontrol.MatchKindFullURL)
		values = append(values, evidence.FullURL)
	}
	if evidence.URLHost != "" {
		kinds = append(kinds, accesscontrol.MatchKindURLHost)
		values = append(values, evidence.URLHost)
	}
	if evidence.ServerIdentity != "" {
		kinds = append(kinds, accesscontrol.MatchKindServerIdentity)
		values = append(values, evidence.ServerIdentity)
	}
	return kinds, values
}

func legacyShadowMCPAccessRuleAppliesToPolicy(rule accesscontrol.AccessRule, policyID string) bool {
	return rule.ObservedSummary.RiskPolicyID == nil || *rule.ObservedSummary.RiskPolicyID == policyID
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
