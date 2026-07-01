package access

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/accesscontrol"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

const (
	shadowMCPInventoryAccessNone    = "none"
	shadowMCPInventoryAccessAllowed = "allowed"
	shadowMCPInventoryAccessDenied  = "denied"
)

type shadowMCPInventoryAccessState struct {
	ExplicitDisposition  string
	EffectiveDisposition string
	ExplicitRule         *shadowMCPInventoryAccessRuleMatch
	EffectiveRule        *shadowMCPInventoryAccessRuleMatch
	ExplanatoryRules     []shadowMCPInventoryAccessRuleMatch
}

type shadowMCPInventoryAccessRuleMatch struct {
	ID          string
	ProjectID   string
	AccessScope string
	Disposition string
	MatchKind   string
	MatchValue  string
	DisplayName string
}

func (s *Service) resolveShadowMCPInventoryAccessState(ctx context.Context, organizationID string, projectID string, inventoryURL shadowmcp.InventoryURL) (shadowMCPInventoryAccessState, error) {
	state := shadowMCPInventoryAccessState{
		ExplicitDisposition:  shadowMCPInventoryAccessNone,
		EffectiveDisposition: shadowMCPInventoryAccessNone,
		ExplicitRule:         nil,
		EffectiveRule:        nil,
		ExplanatoryRules:     nil,
	}

	matchKinds, matchValues := shadowMCPInventoryAccessMatchCandidates(inventoryURL)
	if len(matchKinds) == 0 {
		return state, nil
	}

	rules, err := s.accessStore.ListMatchingRules(ctx, accesscontrol.MatchingRuleFilters{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ResourceType:   accesscontrol.ResourceTypeShadowMCP,
		MatchKinds:     matchKinds,
		MatchValues:    matchValues,
	})
	if err != nil {
		return state, fmt.Errorf("list matching shadow mcp inventory access rules: %w", err)
	}

	state.ExplicitDisposition, state.ExplicitRule = resolveShadowMCPInventoryExplicitRule(rules, projectID, inventoryURL.CanonicalURL)
	state.EffectiveDisposition, state.EffectiveRule = resolveShadowMCPInventoryEffectiveRule(rules)

	explanatoryRules, err := s.listShadowMCPInventoryExplanatoryRules(ctx, organizationID, projectID, inventoryURL)
	if err != nil {
		return state, err
	}
	state.ExplanatoryRules = explanatoryRules

	return state, nil
}

func shadowMCPInventoryAccessMatchCandidates(inventoryURL shadowmcp.InventoryURL) ([]string, []string) {
	evidence := shadowmcp.NormalizeAccessEvidence(shadowmcp.AccessEvidenceForInventoryURL(inventoryURL))
	kinds := make([]string, 0, 2)
	values := make([]string, 0, 2)
	if evidence.FullURL != "" {
		kinds = append(kinds, accesscontrol.MatchKindFullURL)
		values = append(values, evidence.FullURL)
	}
	if evidence.URLHost != "" {
		kinds = append(kinds, accesscontrol.MatchKindURLHost)
		values = append(values, evidence.URLHost)
	}
	return kinds, values
}

func resolveShadowMCPInventoryExplicitRule(rules []accesscontrol.AccessRule, projectID string, canonicalURL string) (string, *shadowMCPInventoryAccessRuleMatch) {
	for _, rule := range rules {
		if !shadowMCPInventoryExplicitURLRule(rule, projectID, canonicalURL) {
			continue
		}
		if rule.Disposition == accesscontrol.DispositionDenied {
			match := shadowMCPInventoryRuleMatch(rule)
			return shadowMCPInventoryAccessDenied, &match
		}
	}

	for _, rule := range rules {
		if !shadowMCPInventoryExplicitURLRule(rule, projectID, canonicalURL) {
			continue
		}
		if rule.Disposition == accesscontrol.DispositionAllowed {
			match := shadowMCPInventoryRuleMatch(rule)
			return shadowMCPInventoryAccessAllowed, &match
		}
	}

	return shadowMCPInventoryAccessNone, nil
}

func resolveShadowMCPInventoryEffectiveRule(rules []accesscontrol.AccessRule) (string, *shadowMCPInventoryAccessRuleMatch) {
	for _, rule := range rules {
		if rule.Disposition == accesscontrol.DispositionDenied {
			match := shadowMCPInventoryRuleMatch(rule)
			return shadowMCPInventoryAccessDenied, &match
		}
	}

	for _, rule := range rules {
		if rule.Disposition == accesscontrol.DispositionAllowed {
			match := shadowMCPInventoryRuleMatch(rule)
			return shadowMCPInventoryAccessAllowed, &match
		}
	}

	return shadowMCPInventoryAccessNone, nil
}

func shadowMCPInventoryExplicitURLRule(rule accesscontrol.AccessRule, projectID string, canonicalURL string) bool {
	return rule.AccessScope == accesscontrol.AccessScopeProject &&
		rule.ProjectID == projectID &&
		rule.MatchKind == accesscontrol.MatchKindFullURL &&
		accesscontrol.CanonicalizeMatchValue(rule.MatchKind, rule.MatchValue) == canonicalURL
}

func (s *Service) listShadowMCPInventoryExplanatoryRules(ctx context.Context, organizationID string, projectID string, inventoryURL shadowmcp.InventoryURL) ([]shadowMCPInventoryAccessRuleMatch, error) {
	filters := []accesscontrol.RuleFilters{
		{
			OrganizationID: organizationID,
			ProjectID:      "",
			AccessScope:    accesscontrol.AccessScopeOrganization,
			ResourceType:   accesscontrol.ResourceTypeShadowMCP,
			Disposition:    "",
			Limit:          shadowMCPMaxPageLimit,
			Cursor:         "",
		},
		{
			OrganizationID: organizationID,
			ProjectID:      projectID,
			AccessScope:    accesscontrol.AccessScopeProject,
			ResourceType:   accesscontrol.ResourceTypeShadowMCP,
			Disposition:    "",
			Limit:          shadowMCPMaxPageLimit,
			Cursor:         "",
		},
	}

	matches := make([]shadowMCPInventoryAccessRuleMatch, 0)
	for _, filter := range filters {
		for {
			result, err := s.accessStore.ListRules(ctx, filter)
			if err != nil {
				return nil, fmt.Errorf("list shadow mcp inventory explanatory access rules: %w", err)
			}
			for _, rule := range result.Rules {
				if rule.MatchKind != accesscontrol.MatchKindServerIdentity {
					continue
				}
				if !shadowMCPInventoryObservedSummaryMatchesURL(rule.ObservedSummary, inventoryURL) {
					continue
				}
				matches = append(matches, shadowMCPInventoryRuleMatch(rule))
			}
			if result.NextCursor == "" {
				break
			}
			filter.Cursor = result.NextCursor
		}
	}

	return matches, nil
}

func shadowMCPInventoryObservedSummaryMatchesURL(summary accesscontrol.ObservedSummary, inventoryURL shadowmcp.InventoryURL) bool {
	if summary.FullURL != nil {
		if observedURL, ok := shadowmcp.CanonicalizeInventoryURL(*summary.FullURL); ok && observedURL.CanonicalURL == inventoryURL.CanonicalURL {
			return true
		}
	}
	if summary.URLHost != nil {
		observedHost, err := shadowmcp.NormalizeMatchValue(shadowmcp.MatchBreadthURLHost, *summary.URLHost)
		if err == nil && observedHost == inventoryURL.URLHost {
			return true
		}
	}
	return false
}

func shadowMCPInventoryRuleMatch(rule accesscontrol.AccessRule) shadowMCPInventoryAccessRuleMatch {
	return shadowMCPInventoryAccessRuleMatch{
		ID:          rule.ID,
		ProjectID:   rule.ProjectID,
		AccessScope: rule.AccessScope,
		Disposition: rule.Disposition,
		MatchKind:   rule.MatchKind,
		MatchValue:  rule.MatchValue,
		DisplayName: rule.DisplayName,
	}
}
