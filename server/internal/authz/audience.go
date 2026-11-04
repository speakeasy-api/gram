package authz

import (
	"context"
	"time"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// OrganizationAdministratorEmails returns the distinct email addresses of
// active organization members who may administer the organization, for
// notifications sent on its behalf. It reads the members' live grants, so it
// needs no request admission; the organization unit's mode for that
// organization chooses which evaluator decides each member. Partial results
// come back with the per-member resolution errors, as with
// ResolveOrganizationAdminEmails.
func (c *Client) OrganizationAdministratorEmails(ctx context.Context, db repo.DBTX, organizationID string) ([]string, error) {
	op := Organization(organizationID).Administer()
	rt := c.router.routeForOrganization(ctx, organizationID, []Operation{op})
	engineAllows := func(grants []Grant) (bool, error) {
		evaluation, err := evaluateGrantCheck(grants, Check{
			Scope:           ScopeOrgAdmin,
			ResourceKind:    "",
			ResourceID:      organizationID,
			Dimensions:      nil,
			selectorMatch:   selectorMatchNormal,
			representedUser: "",
		})
		if err != nil {
			return false, err
		}
		return evaluation.Grant != nil, nil
	}
	registryAllows := func(userID string, grants []Grant) (bool, error) {
		decision, err := c.decide(PolicySet{
			Actor:           urn.NewPrincipal(urn.PrincipalTypeUser, userID),
			Policies:        []Policy{{Principal: "live[0]", Grants: grants}},
			Captured:        nil,
			RepresentedUser: "",
			Represented:     nil,
		}, op)
		if err != nil {
			return false, err
		}
		return decision.Allowed, nil
	}

	switch rt.mode {
	case MigrationModeNew:
		return resolveOrganizationAudience(ctx, db, organizationID, registryAllows)
	case MigrationModeShadow:
		var engineAnswers, registryAnswers []bool
		var registryErr error
		var registryTime time.Duration
		emails, err := resolveOrganizationAudience(ctx, db, organizationID, func(userID string, grants []Grant) (bool, error) {
			allowed, err := engineAllows(grants)
			if err != nil {
				return false, err
			}
			start := time.Now()
			newAllowed, newErr := registryAllows(userID, grants)
			registryTime += time.Since(start)
			if newErr != nil && registryErr == nil {
				registryErr = newErr
			}
			engineAnswers = append(engineAnswers, allowed)
			registryAnswers = append(registryAnswers, newAllowed)
			return allowed, nil
		})
		// A resolution that failed before any member was evaluated compared
		// nothing, so it records nothing.
		if err == nil || len(engineAnswers) > 0 {
			c.shadow.recordAudience(ctx, organizationID, rt, []Operation{op},
				shadowAnswer{allowed: engineAnswers, err: nil},
				shadowAnswer{allowed: registryAnswers, err: registryErr},
				registryTime)
		}
		return emails, err
	default:
		return resolveOrganizationAudience(ctx, db, organizationID, func(_ string, grants []Grant) (bool, error) {
			return engineAllows(grants)
		})
	}
}
