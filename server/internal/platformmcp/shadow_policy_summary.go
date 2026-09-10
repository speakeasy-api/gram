package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

type riskPolicySnapshot struct {
	policy          policycore.Policy
	shadowDecisions *ShadowPolicyDecisions
}

func loadShadowPolicySnapshots(ctx context.Context, db riskrepo.DBTX, policies []policycore.Policy, limit int32) ([]riskPolicySnapshot, error) {
	if len(policies) == 0 {
		return []riskPolicySnapshot{}, nil
	}
	organizationID := policies[0].OrganizationID
	policyIDs := make([]string, 0, len(policies))
	shadowPolicies := make(map[string]struct{}, len(policies))
	displayed := policies
	if len(displayed) > int(limit) {
		displayed = displayed[:limit]
	}
	for _, policy := range displayed {
		if policy.OrganizationID != organizationID {
			return nil, fmt.Errorf("%w: mixed organization policy page", ErrUnavailable)
		}
		if policy.ShadowMCPDisposition != nil {
			policyIDs = append(policyIDs, policy.ID.String())
			shadowPolicies[policy.ID.String()] = struct{}{}
		}
	}
	if len(policyIDs) == 0 {
		result := make([]riskPolicySnapshot, 0, len(policies))
		for _, policy := range policies {
			result = append(result, riskPolicySnapshot{policy: policy, shadowDecisions: nil})
		}
		return result, nil
	}
	allowed, err := authz.ListGrantsForResourceIDs(ctx, db, organizationID, authz.ScopeRiskPolicyBypass, policyIDs)
	if err != nil {
		return nil, fmt.Errorf("%w: list shadow MCP allowed URL grants", ErrUnavailable)
	}
	blocked, err := authz.ListGrantsForResourceIDs(ctx, db, organizationID, authz.ScopeRiskPolicyBlock, policyIDs)
	if err != nil {
		return nil, fmt.Errorf("%w: list shadow MCP blocked URL grants", ErrUnavailable)
	}
	allowedByPolicy := grantsByPolicy(allowed, shadowPolicies)
	blockedByPolicy := grantsByPolicy(blocked, shadowPolicies)
	result := make([]riskPolicySnapshot, 0, len(policies))
	for _, policy := range policies {
		if _, displayed := shadowPolicies[policy.ID.String()]; !displayed {
			result = append(result, riskPolicySnapshot{policy: policy, shadowDecisions: nil})
			continue
		}
		decisions, err := shadowPolicyDecisions(policy, allowedByPolicy[policy.ID.String()], blockedByPolicy[policy.ID.String()])
		if err != nil {
			return nil, err
		}
		result = append(result, riskPolicySnapshot{policy: policy, shadowDecisions: decisions})
	}
	return result, nil
}

func grantsByPolicy(grants []authz.Grant, allowed map[string]struct{}) map[string][]authz.Grant {
	result := make(map[string][]authz.Grant)
	for _, grant := range grants {
		policyID := grant.Selector.ResourceID()
		if _, ok := allowed[policyID]; ok {
			result[policyID] = append(result[policyID], grant)
		}
	}
	return result
}

// ShadowPolicyDecisions describes the configured URL-target posture of a
// blocking Shadow MCP policy. It never contains URLs, principals, or selectors.
type ShadowPolicyDecisions struct {
	Disposition        string `json:"disposition"`
	AllowedTargetCount int    `json:"allowed_target_count"`
	BlockedTargetCount int    `json:"blocked_target_count"`
	ManagedVia         string `json:"managed_via"`
}

func shadowPolicyDecisions(policy policycore.Policy, allowed, blocked []authz.Grant) (*ShadowPolicyDecisions, error) {
	if policy.ShadowMCPDisposition == nil {
		return nil, nil
	}
	return shadowPolicyDecisionsForURLs(*policy.ShadowMCPDisposition, grantURLs(allowed), grantURLs(blocked))
}

func shadowPolicyDecisionsFromVersionState(state RiskPolicyVersionState) (*ShadowPolicyDecisions, error) {
	if state.Policy.ShadowMCPDisposition == nil {
		return nil, nil
	}
	allowed, err := versionGrantURLs(state.AllowedURLGrants)
	if err != nil {
		return nil, err
	}
	blocked, err := versionGrantURLs(state.BlockedURLGrants)
	if err != nil {
		return nil, err
	}
	return shadowPolicyDecisionsForURLs(*state.Policy.ShadowMCPDisposition, allowed, blocked)
}

func shadowPolicyDecisionsForURLs(disposition string, allowed, blocked []string) (*ShadowPolicyDecisions, error) {
	switch disposition {
	case shadowmcp.DispositionBlockAll, shadowmcp.DispositionAllowAll:
	default:
		return nil, fmt.Errorf("%w: unknown shadow MCP disposition", ErrUnavailable)
	}
	allowedCount, err := canonicalTargetCount(allowed)
	if err != nil {
		return nil, err
	}
	blockedCount, err := canonicalTargetCount(blocked)
	if err != nil {
		return nil, err
	}
	return &ShadowPolicyDecisions{
		Disposition: disposition, AllowedTargetCount: allowedCount, BlockedTargetCount: blockedCount, ManagedVia: "shadow_inventory",
	}, nil
}

func grantURLs(grants []authz.Grant) []string {
	urls := make([]string, 0, len(grants))
	for _, grant := range grants {
		if url := grant.Selector[authz.SelectorKeyServerURL]; url != "" {
			urls = append(urls, url)
		}
	}
	return urls
}

func versionGrantURLs(grants []RiskPolicyVersionGrant) ([]string, error) {
	urls := make([]string, 0, len(grants))
	for _, grant := range grants {
		var selector authz.Selector
		if err := json.Unmarshal(grant.Selector, &selector); err != nil {
			return nil, fmt.Errorf("%w: decode shadow MCP URL grant selector", ErrUnavailable)
		}
		if url := selector[authz.SelectorKeyServerURL]; url != "" {
			urls = append(urls, url)
		}
	}
	return urls, nil
}

func canonicalTargetCount(urls []string) (int, error) {
	canonical := make([]string, 0, len(urls))
	for _, url := range urls {
		inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(url)
		if !ok {
			return 0, fmt.Errorf("%w: invalid shadow MCP URL grant", ErrUnavailable)
		}
		canonical = append(canonical, inventoryURL.CanonicalURL)
	}
	slices.Sort(canonical)
	return len(slices.Compact(canonical)), nil
}
