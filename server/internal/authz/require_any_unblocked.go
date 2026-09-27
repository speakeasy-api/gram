package authz

import (
	"context"

	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
)

// RequireAnyUnblocked accepts any allow alternative, but rejects explicit
// exclusions matching ANY supplied check first. Use it when project-wide and
// resource-specific grants authorize the same operation: a broad allow must
// not bypass an exclusion on the actual resource (or vice versa).
//
// Checks carry base scopes, not blocked scopes, and must include the concrete
// resource dimensions. Exclusions use strict selector matching and never expand
// to root. Every admitted policy can exclude; one allow alternative must still
// be satisfied by all policies, just as with RequireAny.
func (e *Engine) RequireAnyUnblocked(ctx context.Context, checks ...Check) error {
	enforce, err := e.ShouldEnforce(ctx)
	if err != nil {
		return err
	}
	if !enforce {
		return nil
	}
	if len(checks) == 0 {
		return e.mapError(ctx, ErrNoChecks)
	}
	authorization, ok := grantAuthorizationFromContext(ctx)
	if !ok {
		return e.mapError(ctx, ErrMissingGrants)
	}
	for _, check := range checks {
		if err := validateInput(check); err != nil {
			return e.mapError(ctx, err)
		}
	}
	for _, check := range checks {
		exclusionScope, ok := ExclusionScopeFor(check.Scope)
		if !ok {
			continue
		}
		exclusion := check.WithStrictSelectorMatch()
		exclusion.Scope = exclusionScope
		// Preserve the base resource kind, including explicit overrides.
		if exclusion.ResourceKind == "" {
			exclusion.ResourceKind = ResourceKindForScope(check.Scope)
		}
		for _, policy := range authorization.policies {
			grant, _ := matchingGrant(policy, expandWithoutRoot(exclusion))
			if grant == nil {
				continue
			}
			challengeLogger{
				Operation:            authzrepo.OperationRequireAny,
				Outcome:              authzrepo.OutcomeDeny,
				Reason:               authzrepo.ReasonDenyGrant,
				Checks:               checks,
				Focus:                &check,
				Matches:              nil,
				EvaluatedGrantCount:  uint32(authorization.grantCount()), //nolint:gosec // grant count is small
				FilterCandidateCount: 0,
				FilterAllowedCount:   0,
			}.Log(ctx, e.db, e.logger, e.challengeLoggingEnabled)
			return e.mapError(ctx, Denied(check.Scope, check.selector()))
		}
	}
	return e.RequireAny(ctx, checks...)
}
