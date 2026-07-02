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
	Access string
	Rule   *shadowMCPInventoryAccessRuleMatch
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
		Access: shadowMCPInventoryAccessNone,
		Rule:   nil,
	}

	if inventoryURL.CanonicalURL == "" {
		return state, nil
	}

	rules, err := s.accessStore.ListMatchingRules(ctx, accesscontrol.MatchingRuleFilters{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ResourceType:   accesscontrol.ResourceTypeShadowMCP,
		MatchKinds:     []string{accesscontrol.MatchKindFullURL},
		MatchValues:    []string{inventoryURL.CanonicalURL},
	})
	if err != nil {
		return state, fmt.Errorf("list matching shadow mcp inventory access rules: %w", err)
	}

	state.Access, state.Rule = resolveShadowMCPInventoryURLRule(rules)

	return state, nil
}

func resolveShadowMCPInventoryURLRule(rules []accesscontrol.AccessRule) (string, *shadowMCPInventoryAccessRuleMatch) {
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
