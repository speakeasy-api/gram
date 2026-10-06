package authz

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

type contextKey string

const (
	grantsContextKey contextKey = "authz_grants"
	// admittedPoliciesContextKey holds the policy sets of whichever admission
	// ran last. Principal credential and workload admission share it, so a
	// later admission always replaces an earlier one instead of being shadowed
	// by it.
	admittedPoliciesContextKey contextKey = "authz_admitted_policies"
)

// grantAuthorization is the set of independent policies a caller acts under.
// Every set must allow a check for it to pass.
type grantAuthorization struct {
	policies [][]Grant
}

// GrantsToContext stores resolved grants on the request context.
func GrantsToContext(ctx context.Context, grants []Grant) context.Context {
	return context.WithValue(ctx, grantsContextKey, grants)
}

// GrantsFromContext loads resolved grants from the request context.
func GrantsFromContext(ctx context.Context) ([]Grant, bool) {
	grants, ok := ctx.Value(grantsContextKey).([]Grant)
	return grants, ok
}

func admittedPoliciesToContext(ctx context.Context, sets ...[]Grant) context.Context {
	policies := grantAuthorization{policies: make([][]Grant, 0, len(sets))}
	for _, set := range sets {
		policies.policies = append(policies.policies, append([]Grant(nil), set...))
	}
	return context.WithValue(ctx, admittedPoliciesContextKey, policies)
}

func grantAuthorizationFromContext(ctx context.Context) (grantAuthorization, bool) {
	if policies, ok := ctx.Value(admittedPoliciesContextKey).(grantAuthorization); ok {
		return policies, true
	}
	if _, principalCredential := contextvalues.PrincipalCredentialAuthorization(ctx); principalCredential {
		return grantAuthorization{policies: nil}, false
	}
	if mode, ok := contextvalues.APIKeyAuthorization(ctx); ok && mode == contextvalues.APIKeyAuthorizationModePrincipal {
		return grantAuthorization{policies: nil}, false
	}
	grants, ok := GrantsFromContext(ctx)
	if !ok {
		return grantAuthorization{policies: nil}, false
	}
	return grantAuthorization{policies: [][]Grant{grants}}, true
}

func loadedGrantAuthorization(grants []Grant) grantAuthorization {
	return grantAuthorization{policies: [][]Grant{grants}}
}

func (a grantAuthorization) grantCount() int {
	total := 0
	for _, policy := range a.policies {
		total += len(policy)
	}
	return total
}

func (a grantAuthorization) evaluate(check Check) (grantCheckEvaluation, error) {
	if len(a.policies) == 0 {
		return grantCheckEvaluation{Grant: nil, Check: nil, Denied: false}, nil
	}

	allowed := true
	denied := false
	var representative grantCheckEvaluation
	for _, policy := range a.policies {
		evaluation, err := evaluateGrantCheck(policy, check)
		if err != nil {
			return grantCheckEvaluation{Grant: nil, Check: nil, Denied: false}, err
		}
		denied = denied || evaluation.Denied
		if evaluation.Grant == nil {
			allowed = false
			continue
		}
		if representative.Grant == nil {
			representative = evaluation
		}
	}
	if !allowed {
		return grantCheckEvaluation{Grant: nil, Check: nil, Denied: denied}, nil
	}
	return representative, nil
}
