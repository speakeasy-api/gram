package authz

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// A credential that acts for a user carries that user's live policy. Today it
// bounds auth-client checks only. It is meant to bound every check, so that a
// credential never does what the user who authorized it may no longer do.
// Until that is enforced, each allowed decision is made again with that
// user's policies added, and a decision that would flip is reported here.

const authorizerWouldRefuseLog = "authz check passes the credential's policies but not the live policy of the user it acts for"

// boundedByAuthorizer is the authorization a credential would have if the
// policies of the user it acts for bounded every check. Each policy stays
// separate, so a grant in one never cancels a block in another: a check
// passes only when every policy allows it.
func (a grantAuthorization) boundedByAuthorizer() (grantAuthorization, bool) {
	if len(a.representedPolicies) == 0 {
		return a, false
	}
	bounded := a
	bounded.policies = append(append([][]Grant{}, a.policies...), a.representedPolicies...)
	return bounded, true
}

// authorizerRefusesEach returns the first allowed check the bounded
// authorization refuses. Auth-client checks are left out: they are already
// bounded by the authorizer's policies.
func (a grantAuthorization) authorizerRefusesEach(allowed []Check) (*Check, error) {
	bounded, ok := a.boundedByAuthorizer()
	if !ok {
		return nil, nil
	}
	for _, check := range allowed {
		if check.isAuthClientCheck() {
			continue
		}
		evaluation, err := bounded.evaluate(check)
		if err != nil {
			return nil, err
		}
		if evaluation.Grant == nil {
			return &check, nil
		}
	}
	return nil, nil
}

// authorizerRefusesAny reports whether the bounded authorization allows no
// alternative on its own. When it refuses, it returns the first alternative
// the credential allows.
func (a grantAuthorization) authorizerRefusesAny(alternatives []Check) (*Check, error) {
	bounded, ok := a.boundedByAuthorizer()
	if !ok || len(alternatives) == 0 {
		return nil, nil
	}
	decision, err := bounded.decideAnyOf(alternatives)
	if err != nil || decision.allowed() {
		return nil, err
	}
	for _, alternative := range alternatives {
		evaluation, err := a.evaluate(alternative)
		if err != nil {
			return nil, err
		}
		if evaluation.Grant != nil {
			return &alternative, nil
		}
	}
	return &alternatives[0], nil
}

// authorizerRefusesGroup reports whether the bounded authorization refuses an
// Either group, where a block on any alternative refuses the whole group. For
// example, a delegator with a block on server two refuses Either(two, one)
// even though nothing blocks server one.
func (a grantAuthorization) authorizerRefusesGroup(alternatives []Check) (*Check, error) {
	bounded, ok := a.boundedByAuthorizer()
	if !ok || len(alternatives) == 0 {
		return nil, nil
	}
	blocked, err := firstUnoverriddenExclusion(bounded, alternatives)
	if err != nil || blocked != nil {
		return blocked, err
	}
	return a.authorizerRefusesAny(alternatives)
}

// reportAuthorizerRefusal logs, once per engine call, that the call was
// allowed although the authorizer's policies would refuse it. The decision is
// unchanged.
func reportAuthorizerRefusal(ctx context.Context, logger *slog.Logger, authorization grantAuthorization, refused *Check, err error) {
	if err != nil {
		logger.ErrorContext(ctx, "authz check could not be evaluated against the policy of the user the credential acts for", attr.SlogUserID(authorization.representedUser), attr.SlogError(err))
		return
	}
	if refused == nil {
		return
	}
	logger.WarnContext(
		ctx,
		authorizerWouldRefuseLog,
		attr.SlogUserID(authorization.representedUser),
		attr.SlogResourceID(refused.ResourceID),
		attr.SlogAccessRequestScope(string(refused.Scope)),
	)
}

func (e *Engine) reportAuthorizerRefusals(ctx context.Context, authorization grantAuthorization, allowed []Check) {
	refused, err := authorization.authorizerRefusesEach(allowed)
	reportAuthorizerRefusal(ctx, e.logger, authorization, refused, err)
}
