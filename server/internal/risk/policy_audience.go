package risk

import (
	"context"
	"fmt"
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
		return nil, nil
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

	return authz.ReplaceGrantsForResource(ctx, db, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyEvaluate,
			ResourceID:     policyID,
		},
		Principals: principals,
	})
}

func riskPolicyAudiencePrincipalURNs(ctx context.Context, db repo.DBTX, organizationID string, policyID string) ([]string, error) {
	grants, err := authz.ListGrantsForResource(ctx, db, authz.Resource{
		OrganizationID: organizationID,
		Scope:          authz.ScopeRiskPolicyEvaluate,
		ResourceID:     policyID,
	})
	if err != nil {
		return nil, err
	}

	principalURNs := make([]string, 0, len(grants))
	for _, grant := range grants {
		if grant.Effect != authz.PolicyEffectAllow {
			continue
		}
		principalURNs = append(principalURNs, grant.PrincipalUrn)
	}
	slices.Sort(principalURNs)
	principalURNs = slices.Compact(principalURNs)

	return principalURNs, nil
}

func riskPolicyAppliesToUserGrants(policyID string, grants []authz.Grant) bool {
	check := authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID)
	for _, grant := range grants {
		if grant.Scope != authz.ScopeRiskPolicyEvaluate || grant.Effect != authz.PolicyEffectAllow {
			continue
		}
		if grant.Selector.Matches(check) {
			return true
		}
	}
	return false
}
