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

// authorizerRefuses reports whether the live policy of the user the credential
// acts for refuses a check. Auth-client checks are left out: that policy
// already decides them.
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

// authorizerWouldRefuse returns the allowed checks the authorizer's live
// policy refuses. With anyOf it returns them only when that policy refuses
// every allowed alternative, since one surviving alternative keeps the
// request allowed.
func (a grantAuthorization) authorizerWouldRefuse(allowed []Check, anyOf bool) []Check {
	if len(a.representedPolicies) == 0 {
		return nil
	}
	var refused []Check
	for _, check := range allowed {
		refuses, err := a.authorizerRefuses(check)
		if err != nil {
			continue
		}
		if !refuses && anyOf {
			return nil
		}
		if refuses {
			refused = append(refused, check)
		}
	}
	return refused
}

// reportAuthorizerRefusals logs, once per request, that a request was allowed
// checks the authorizer's live policy refuses, naming the first of them. The
// decision is unchanged.
func (e *Engine) reportAuthorizerRefusals(ctx context.Context, authorization grantAuthorization, allowed []Check, anyOf bool) {
	refused := authorization.authorizerWouldRefuse(allowed, anyOf)
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
