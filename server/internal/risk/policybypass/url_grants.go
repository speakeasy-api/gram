package policybypass

import (
	"context"
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/authz"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type ReconcilePolicyURLsInput struct {
	OrganizationID string
	PolicyID       string
	// DesiredURLs nil preserves the existing URL set while refreshing its audience.
	DesiredURLs []string
	Principals  []urn.Principal
}

func URLSelector(policyID, canonicalURL string) authz.Selector {
	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID)
	selector[authz.SelectorKeyServerURL] = canonicalURL
	return selector
}

func CanonicalizeURLs(rawURLs []string) ([]string, error) {
	canonical := make([]string, 0, len(rawURLs))
	for _, rawURL := range rawURLs {
		inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(rawURL)
		if !ok {
			return nil, fmt.Errorf("invalid shadow mcp server url: %q", rawURL)
		}
		canonical = append(canonical, inventoryURL.CanonicalURL)
	}
	slices.Sort(canonical)
	return slices.Compact(canonical), nil
}

func ReconcilePolicyURLs(ctx context.Context, db riskrepo.DBTX, input ReconcilePolicyURLsInput) error {
	grants, err := authz.ListGrantsForResource(ctx, db, authz.Resource{
		OrganizationID: input.OrganizationID,
		Scope:          authz.ScopeRiskPolicyBypass,
		ResourceID:     input.PolicyID,
	})
	if err != nil {
		return fmt.Errorf("list policy url grants: %w", err)
	}

	existing := make(map[string]struct{}, len(grants))
	existingGrants := make(map[string][]authz.Grant, len(grants))
	for _, grant := range grants {
		if grant.Effect != authz.PolicyEffectAllow {
			continue
		}
		serverURL := grant.Selector[authz.SelectorKeyServerURL]
		if serverURL == "" {
			continue
		}
		existing[serverURL] = struct{}{}
		existingGrants[serverURL] = append(existingGrants[serverURL], grant)
	}

	desired := make(map[string]struct{}, len(input.DesiredURLs))
	if input.DesiredURLs == nil {
		for serverURL := range existing {
			desired[serverURL] = struct{}{}
		}
	} else {
		for _, serverURL := range input.DesiredURLs {
			desired[serverURL] = struct{}{}
		}
	}

	toAdd := make(map[string]struct{}, len(desired))
	retained := make(map[string]struct{}, len(desired))
	for serverURL := range desired {
		if _, ok := existing[serverURL]; ok {
			retained[serverURL] = struct{}{}
			continue
		}
		toAdd[serverURL] = struct{}{}
	}

	toRemove := make(map[string]struct{}, len(existing))
	for serverURL := range existing {
		if _, ok := desired[serverURL]; !ok {
			toRemove[serverURL] = struct{}{}
		}
	}

	for _, serverURL := range sortedURLSet(toRemove) {
		if err := revokePolicyURLGrants(ctx, db, input.OrganizationID, input.PolicyID, existingGrants[serverURL]); err != nil {
			return fmt.Errorf("revoke removed policy url %q: %w", serverURL, err)
		}
	}

	for _, serverURL := range sortedURLSet(retained) {
		if err := revokePolicyURLGrants(ctx, db, input.OrganizationID, input.PolicyID, existingGrants[serverURL]); err != nil {
			return fmt.Errorf("revoke retained policy url %q variants: %w", serverURL, err)
		}
		if err := ReplacePolicyURLAudience(ctx, db, input.OrganizationID, input.PolicyID, serverURL, input.Principals); err != nil {
			return fmt.Errorf("replace policy url %q audience: %w", serverURL, err)
		}
	}

	for _, serverURL := range sortedURLSet(toAdd) {
		if err := ReplacePolicyURLAudience(ctx, db, input.OrganizationID, input.PolicyID, serverURL, input.Principals); err != nil {
			return fmt.Errorf("replace policy url %q audience: %w", serverURL, err)
		}
	}

	return nil
}

func revokePolicyURLGrants(
	ctx context.Context,
	db riskrepo.DBTX,
	organizationID string,
	policyID string,
	grants []authz.Grant,
) error {
	for _, grant := range grants {
		principal, err := urn.ParsePrincipal(grant.PrincipalUrn)
		if err != nil {
			return fmt.Errorf("parse policy url grant principal: %w", err)
		}
		if err := authz.RevokeResourceFromPrincipals(ctx, db, authz.ResourceGrant{
			Resource: authz.Resource{
				OrganizationID: organizationID,
				Scope:          authz.ScopeRiskPolicyBypass,
				ResourceID:     policyID,
			},
			Effect:     authz.PolicyEffectAllow,
			Principals: []urn.Principal{principal},
			Selector:   grant.Selector,
		}); err != nil {
			return fmt.Errorf("revoke policy url grant selector: %w", err)
		}
	}

	return nil
}

func ReplacePolicyURLAudience(
	ctx context.Context,
	db riskrepo.DBTX,
	organizationID string,
	policyID string,
	canonicalURL string,
	principals []urn.Principal,
) error {
	if err := authz.ReplaceGrantAudience(ctx, db, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID,
		},
		Effect:     authz.PolicyEffectAllow,
		Principals: principals,
		Selector:   URLSelector(policyID, canonicalURL),
	}); err != nil {
		return fmt.Errorf("replace policy url audience: %w", err)
	}

	return nil
}

func RevokePolicyURL(
	ctx context.Context,
	db riskrepo.DBTX,
	organizationID string,
	policyID string,
	canonicalURL string,
) error {
	if err := authz.ReplaceGrantAudience(ctx, db, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID,
		},
		Effect:     authz.PolicyEffectAllow,
		Principals: nil,
		Selector:   URLSelector(policyID, canonicalURL),
	}); err != nil {
		return fmt.Errorf("revoke policy url: %w", err)
	}

	return nil
}

func sortedURLSet(urls map[string]struct{}) []string {
	result := make([]string, 0, len(urls))
	for serverURL := range urls {
		result = append(result, serverURL)
	}
	slices.Sort(result)
	return result
}
