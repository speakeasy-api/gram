package shadowmcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	AccessRuleOutcomeAllowed = "allowed"
	AccessRuleOutcomeDenied  = "denied"
	AccessRuleOutcomeNoMatch = "no_match"
	AccessRuleOutcomeError   = "error"
)

type AccessRuleDecision struct {
	Outcome string
	RuleID  string
	Reason  string
}

func (d AccessRuleDecision) Allows() bool {
	return d.Outcome == AccessRuleOutcomeAllowed
}

func (c *Client) EvaluateAccessRules(ctx context.Context, organizationID string, projectID string, evidence AccessEvidence) AccessRuleDecision {
	normalized := NormalizeAccessEvidence(evidence)
	if normalized.Empty() {
		return AccessRuleDecision{
			Outcome: AccessRuleOutcomeNoMatch,
			RuleID:  "",
			Reason:  "no Shadow MCP URL was available for Access Rule matching",
		}
	}
	parsedProjectID, err := uuid.Parse(projectID)
	if err != nil {
		c.logger.WarnContext(ctx, "failed to parse shadow mcp project id",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProjectID(projectID),
		)
		return AccessRuleDecision{
			Outcome: AccessRuleOutcomeError,
			RuleID:  "",
			Reason:  "failed to evaluate Shadow MCP Access Rules",
		}
	}

	matchKinds, matchValues := accessRuleMatchCandidates(normalized)
	rules, err := c.accessStore.ListMatchingRules(ctx, accesscontrol.MatchingRuleFilters{
		OrganizationID: organizationID,
		ResourceType:   accesscontrol.ResourceTypeShadowMCP,
		ProjectID:      parsedProjectID.String(),
		MatchKinds:     matchKinds,
		MatchValues:    matchValues,
	})
	if err != nil {
		c.logger.WarnContext(ctx, "failed to list matching shadow mcp access rules",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProjectID(projectID),
		)
		return AccessRuleDecision{
			Outcome: AccessRuleOutcomeError,
			RuleID:  "",
			Reason:  "failed to evaluate Shadow MCP Access Rules",
		}
	}

	for _, rule := range rules {
		if rule.Disposition == accesscontrol.DispositionDenied {
			return AccessRuleDecision{
				Outcome: AccessRuleOutcomeDenied,
				RuleID:  rule.ID,
				Reason:  fmt.Sprintf("matched denied Shadow MCP Access Rule %q", rule.DisplayName),
			}
		}
	}

	for _, rule := range rules {
		if rule.Disposition != accesscontrol.DispositionAllowed {
			continue
		}
		return AccessRuleDecision{
			Outcome: AccessRuleOutcomeAllowed,
			RuleID:  rule.ID,
			Reason:  fmt.Sprintf("matched allowed Shadow MCP Access Rule %q", rule.DisplayName),
		}
	}

	return AccessRuleDecision{
		Outcome: AccessRuleOutcomeNoMatch,
		RuleID:  "",
		Reason:  "no matching Shadow MCP Access Rule was found",
	}
}

func (c *Client) CanBypassLegacyPolicyAccess(ctx context.Context, organizationID string, projectID string, userID string, policyID string, evidence AccessEvidence) bool {
	if strings.TrimSpace(organizationID) == "" || strings.TrimSpace(projectID) == "" || strings.TrimSpace(policyID) == "" {
		return false
	}

	normalized := NormalizeAccessEvidence(evidence)
	matchKinds, matchValues := legacyAccessRuleMatchCandidates(normalized)
	if len(matchKinds) == 0 {
		return false
	}

	rules, err := c.accessStore.ListMatchingRules(ctx, accesscontrol.MatchingRuleFilters{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ResourceType:   accesscontrol.ResourceTypeShadowMCP,
		MatchKinds:     matchKinds,
		MatchValues:    matchValues,
	})
	if err != nil {
		c.logger.WarnContext(ctx, "failed to list legacy shadow mcp access rules for risk policy bypass",
			attr.SlogError(err),
			attr.SlogOrganizationID(organizationID),
			attr.SlogProjectID(projectID),
			attr.SlogRiskPolicyID(policyID),
		)
		return false
	}

	for _, rule := range rules {
		if rule.Disposition == accesscontrol.DispositionDenied && legacyAccessRuleAppliesToPolicy(rule, policyID) {
			return false
		}
	}

	for _, rule := range rules {
		if rule.Disposition != accesscontrol.DispositionAllowed || !legacyAccessRuleAppliesToPolicy(rule, policyID) {
			continue
		}
		if c.legacyAccessRuleAllowsUser(ctx, organizationID, userID, rule) {
			return true
		}
	}

	return false
}

func (c *Client) legacyAccessRuleAllowsUser(ctx context.Context, organizationID string, userID string, rule accesscontrol.AccessRule) bool {
	if strings.TrimSpace(rule.SourceRequestID) == "" {
		return true
	}
	if strings.TrimSpace(userID) == "" {
		return false
	}

	request, err := c.accessStore.GetRequest(ctx, organizationID, accesscontrol.ResourceTypeShadowMCP, rule.SourceRequestID)
	if err != nil {
		if !errors.Is(err, accesscontrol.ErrNotFound) {
			c.logger.WarnContext(ctx, "failed to load legacy shadow mcp source request for risk policy bypass",
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

func legacyAccessRuleMatchCandidates(evidence AccessEvidence) ([]string, []string) {
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

func legacyAccessRuleAppliesToPolicy(rule accesscontrol.AccessRule, policyID string) bool {
	return rule.ObservedSummary.RiskPolicyID == nil || *rule.ObservedSummary.RiskPolicyID == policyID
}

func accessRuleMatchCandidates(evidence AccessEvidence) ([]string, []string) {
	kinds := make([]string, 0, 2)
	values := make([]string, 0, 2)
	if evidence.FullURL != "" {
		kinds = append(kinds, MatchBreadthFullURL)
		values = append(values, evidence.FullURL)
	}
	if evidence.URLHost != "" {
		kinds = append(kinds, MatchBreadthURLHost)
		values = append(values, evidence.URLHost)
	}
	return kinds, values
}
