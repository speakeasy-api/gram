package risk

import (
	"context"
	"fmt"

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

func clearRiskPolicyAudienceGrants(ctx context.Context, db repo.DBTX, organizationID string, policyID string) error {
	if err := authz.ReplaceGrantAudience(ctx, db, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyEvaluate,
			ResourceID:     policyID,
		},
		Principals: nil,
		Selector:   authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID),
	}); err != nil {
		return fmt.Errorf("clear risk policy audience grants: %w", err)
	}

	return nil
}
