package authz

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// A credential that acts for a user carries that user's live policy. Today it
// bounds auth-client checks only. It is meant to bound every check, so that a
// credential never does what the user who authorized it may no longer do.
// Until that is enforced, the checks it would refuse are reported here.

const authorizerWouldRefuseLog = "authz check passes the credential's policies but not the live policy of the user it acts for"

// authorizerRefuses reports whether a policy of the user the credential acts
// for refuses a check: that user's live policy and, under an admission
// boundary, the one admitted with it. These are the policies an auth-client
// check must already pass, which is why auth-client checks are left out.
func (a grantAuthorization) authorizerRefuses(check Check) (bool, error) {
	if len(a.representedPolicies) == 0 || check.isAuthClientCheck() {
		return false, nil
	}
	for _, policy := range a.representedPolicies {
		evaluation, err := evaluateGrantCheck(policy, check)
		if err != nil {
			return false, err
		}
		if evaluation.Grant == nil {
			return true, nil
		}
	}
	return false, nil
}

// authorizerWouldRefuse returns the allowed checks the authorizer's policies
// refuse. With anyOf it returns them only when those policies refuse every
// allowed alternative, since one surviving alternative keeps the request
// allowed. A check that cannot be evaluated is not proven refused: it is left
// out, it counts as a surviving alternative, and its error is returned.
func (a grantAuthorization) authorizerWouldRefuse(allowed []Check, anyOf bool) ([]Check, error) {
	if len(a.representedPolicies) == 0 {
		return nil, nil
	}
	var refused []Check
	var firstErr error
	for _, check := range allowed {
		refuses, err := a.authorizerRefuses(check)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if (err != nil || !refuses) && anyOf {
			return nil, firstErr
		}
		if err == nil && refuses {
			refused = append(refused, check)
		}
	}
	return refused, firstErr
}

// reportAuthorizerRefusals logs, once per engine call, that the call was
// allowed checks the authorizer's policies refuse, naming the first of them.
// The decision is unchanged.
func (e *Engine) reportAuthorizerRefusals(ctx context.Context, authorization grantAuthorization, allowed []Check, anyOf bool) {
	refused, err := authorization.authorizerWouldRefuse(allowed, anyOf)
	if err != nil {
		e.logger.ErrorContext(ctx, "authz check could not be evaluated against the policy of the user the credential acts for", attr.SlogUserID(authorization.representedUser), attr.SlogError(err))
	}
	if len(refused) == 0 {
		return
	}
	e.logger.WarnContext(
		ctx,
		authorizerWouldRefuseLog,
		attr.SlogUserID(authorization.representedUser),
		attr.SlogResourceID(refused[0].ResourceID),
		attr.SlogAccessRequestScope(string(refused[0].Scope)),
	)
}

// allowedAlternatives returns the alternatives the credential's policies
// allow. It is only worked out for a credential that acts for a user.
func (a grantAuthorization) allowedAlternatives(alternatives []Check) []Check {
	if len(a.representedPolicies) == 0 {
		return nil
	}
	var allowed []Check
	for _, check := range alternatives {
		evaluation, err := a.evaluate(check)
		if err == nil && evaluation.Grant != nil && !evaluation.Denied {
			allowed = append(allowed, check)
		}
	}
	return allowed
}
