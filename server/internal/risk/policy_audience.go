package risk

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	riskPolicyAudienceEveryone = "everyone"
	riskPolicyAudienceTargeted = "targeted"
)

func validateRiskPolicyAudienceType(audienceType string) error {
	switch audienceType {
	case riskPolicyAudienceEveryone, riskPolicyAudienceTargeted:
		return nil
	default:
		return fmt.Errorf("invalid policy audience type %q", audienceType)
	}
}

func riskPolicyAudiencePrincipals(audienceType string, principalURNs []string) ([]urn.Principal, error) {
	if err := validateRiskPolicyAudienceType(audienceType); err != nil {
		return nil, err
	}
	if audienceType == riskPolicyAudienceEveryone {
		return []urn.Principal{authz.AllUsersPrincipal()}, nil
	}
	if len(principalURNs) == 0 {
		return nil, fmt.Errorf("targeted policy audience requires at least one principal")
	}

	principals := make([]urn.Principal, 0, len(principalURNs))
	seen := make(map[string]struct{}, len(principalURNs))
	for _, principalURN := range principalURNs {
		principal, err := urn.ParsePrincipal(principalURN)
		if err != nil {
			return nil, fmt.Errorf("parse audience principal: %w", err)
		}
		switch principal.Type {
		case urn.PrincipalTypeUser:
			if principal.ID == urn.AllUsersPrincipalID {
				return nil, fmt.Errorf("targeted policy audience cannot use user:all; use audience_type=everyone")
			}
		case urn.PrincipalTypeRole:
		default:
			return nil, fmt.Errorf("targeted policy audience supports user and role principals only")
		}
		key := principal.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		principals = append(principals, principal)
	}

	return principals, nil
}

func principalStrings(principals []urn.Principal) []string {
	values := make([]string, 0, len(principals))
	for _, principal := range principals {
		values = append(values, principal.String())
	}
	return values
}

func syncRiskPolicyAudienceGrants(ctx context.Context, db repo.DBTX, organizationID string, policyID string, audienceType string, principalURNs []string) error {
	principals, err := riskPolicyAudiencePrincipals(audienceType, principalURNs)
	if err != nil {
		return err
	}

	if err := authz.ReplaceGrantAudience(ctx, db, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyEvaluate,
			ResourceID:     policyID,
		},
		Effect:     authz.PolicyEffectAllow,
		Principals: principals,
		Selector:   authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID),
	}); err != nil {
		return fmt.Errorf("replace risk policy audience grants: %w", err)
	}

	return nil
}

func clearRiskPolicyAudienceGrants(ctx context.Context, db repo.DBTX, organizationID string, policyID string) error {
	if err := authz.ReplaceGrantAudience(ctx, db, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyEvaluate,
			ResourceID:     policyID,
		},
		Effect:     authz.PolicyEffectAllow,
		Principals: nil,
		Selector:   authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID),
	}); err != nil {
		return fmt.Errorf("clear risk policy audience grants: %w", err)
	}

	return nil
}

func riskPolicyAudiencePrincipalURNs(ctx context.Context, db repo.DBTX, organizationID string, policyID string) ([]string, error) {
	grants, err := authz.ListGrantsForResource(ctx, db, authz.Resource{
		OrganizationID: organizationID,
		Scope:          authz.ScopeRiskPolicyEvaluate,
		ResourceID:     policyID,
	})
	if err != nil {
		return nil, fmt.Errorf("list risk policy audience grants: %w", err)
	}

	principalURNs := make([]string, 0, len(grants))
	for _, grant := range grants {
		if grant.Effect != authz.PolicyEffectAllow {
			continue
		}
		if !maps.Equal(grant.Selector, authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID)) {
			continue
		}
		principalURNs = append(principalURNs, grant.PrincipalUrn)
	}
	slices.Sort(principalURNs)
	principalURNs = slices.Compact(principalURNs)

	return principalURNs, nil
}

// riskPolicyAudienceURNsByPolicy batch-loads audience principal URNs for every
// risk policy in an org in a single query, keyed by policy id. Batched form of
// riskPolicyAudiencePrincipalURNs used by ListRiskPolicies to avoid a per-policy
// round trip. Policies with no audience grants are simply absent from the map.
func riskPolicyAudienceURNsByPolicy(ctx context.Context, db repo.DBTX, organizationID string) (map[string][]string, error) {
	grants, err := authz.ListGrantsForScopeKind(ctx, db, organizationID, authz.ScopeRiskPolicyEvaluate)
	if err != nil {
		return nil, fmt.Errorf("list risk policy audience grants: %w", err)
	}

	byPolicy := make(map[string][]string)
	for _, grant := range grants {
		if grant.Effect != authz.PolicyEffectAllow {
			continue
		}
		// Attribute the grant to its policy via the selector's resource_id, then
		// re-check against the canonical selector so grants carrying extra keys
		// (or wildcards) are excluded exactly as the single-policy path does.
		policyID := grant.Selector.ResourceID()
		if !maps.Equal(grant.Selector, authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID)) {
			continue
		}
		byPolicy[policyID] = append(byPolicy[policyID], grant.PrincipalUrn)
	}
	for policyID, principalURNs := range byPolicy {
		slices.Sort(principalURNs)
		byPolicy[policyID] = slices.Compact(principalURNs)
	}

	return byPolicy, nil
}
