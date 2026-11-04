package authz

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// RequireMemberOrganization enforces op, an organization operation, on
// userID's live role membership in that organization rather than on the
// request's prepared policies. It answers for an organization a caller names,
// which the request may not have been admitted into, so each call routes by
// that organization afresh.
func (c *Client) RequireMemberOrganization(ctx context.Context, userID string, op Operation) error {
	return c.RequireMemberOrganizationWithDBTX(ctx, c.engine.db, userID, op)
}

// RequireMemberOrganizationWithDBTX is RequireMemberOrganization reading
// membership on db, which may be a transaction that already holds a source
// lock. As with the engine's live-membership check, principal API keys and
// principal credentials are refused, and an unenforced request passes unless
// an API key authenticated it.
func (c *Client) RequireMemberOrganizationWithDBTX(ctx context.Context, db repo.DBTX, userID string, op Operation) error {
	if err := c.validate([]Operation{op}); err != nil {
		return c.engine.mapError(ctx, err)
	}
	if op.alternatives || op.permission.Type != "organization" {
		return c.engine.mapError(ctx, fmt.Errorf("%w: %s is not an organization operation", ErrInvalidCheck, op))
	}
	check, ok := c.legacyCheck(op)
	if !ok {
		return c.engine.mapError(ctx, fmt.Errorf("%w: operation %s has no engine form", ErrInvalidCheck, op))
	}
	organizationID := op.instance.resourceID
	ops := []Operation{op}
	rt := c.router.routeForOrganization(ctx, organizationID, ops)
	if rt.mode != MigrationModeShadow && rt.mode != MigrationModeNew {
		return c.engine.RequireUserOrganizationScopeWithDBTX(ctx, db, organizationID, userID, check.Scope)
	}

	if mode, ok := contextvalues.APIKeyAuthorization(ctx); ok && mode == contextvalues.APIKeyAuthorizationModePrincipal {
		return oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.PrincipalCredentialAuthorization(ctx); ok {
		return oops.C(oops.CodeForbidden)
	}
	enforce, err := c.engine.ShouldEnforce(ctx)
	if err != nil {
		return err
	}
	if !enforce {
		if authCtx, _ := contextvalues.GetAuthContext(ctx); authCtx.APIKeyID != "" {
			return oops.C(oops.CodeForbidden)
		}
		return nil
	}

	principals, err := ResolveUserPrincipals(ctx, db, organizationID, userID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "resolve requested organization principals").LogError(ctx, c.engine.logger, attr.SlogOrganizationID(organizationID), attr.SlogUserID(userID))
	}
	grants, err := LoadGrants(ctx, db, organizationID, principals)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load requested organization grants").LogError(ctx, c.engine.logger, attr.SlogOrganizationID(organizationID), attr.SlogUserID(userID))
	}
	set := PolicySet{
		Actor:           urn.NewPrincipal(urn.PrincipalTypeUser, userID),
		Policies:        []Policy{{Principal: "live[0]", Grants: grants}},
		Captured:        nil,
		RepresentedUser: "",
		Represented:     nil,
	}

	if rt.mode == MigrationModeShadow {
		err := c.engine.EvaluateLoadedGrants(ctx, grants, check)
		c.shadow.compare(ctx, "require_member", rt, ops, requireAnswer(err), func() shadowAnswer {
			decision, err := c.decide(set, op)
			return shadowAnswer{allowed: []bool{decision.Allowed}, err: err}
		})
		return err
	}

	checks := []Check{check}
	decision, err := c.decide(set, op)
	if err != nil {
		c.log(ctx, authzrepo.OperationRequire, authzrepo.OutcomeError, authzrepo.ReasonInvalidCheck, set, checks, &checks[0], nil)
		return c.engine.mapError(ctx, err)
	}
	if !decision.Allowed {
		c.log(ctx, authzrepo.OperationRequire, authzrepo.OutcomeDeny, denialReason(decision, set), set, checks, &checks[0], nil)
		return c.engine.mapError(ctx, Denied(check.Scope, check.selector()))
	}
	var matches []grantMatch
	if decision.Grant != nil {
		matches = []grantMatch{{Grant: *decision.Grant, ViaCheck: check}}
	}
	c.log(ctx, authzrepo.OperationRequire, authzrepo.OutcomeAllow, authzrepo.ReasonGrantMatched, set, checks, &checks[0], matches)
	return nil
}
