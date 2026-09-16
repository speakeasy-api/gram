package authz

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

type contextKey string

const (
	grantsContextKey                      contextKey = "authz_grants"
	principalCredentialPoliciesContextKey contextKey = "authz_principal_credential_policies" //nolint:gosec // private context key, not credential material
	workloadPoliciesContextKey            contextKey = "authz_workload_policies"
)

type principalCredentialPolicies struct {
	credential []Grant
	agent      []Grant
	owner      []Grant
}

// workloadPolicies is the two-set form a workload session acts under: the
// session ceiling and the assigned agent's live policy. Kept separate from
// principalCredentialPolicies rather than storing an empty owner set, which the
// evaluator would read as "the owner allows nothing" and deny every check.
type workloadPolicies struct {
	ceiling []Grant
	agent   []Grant
}

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

func principalCredentialPoliciesToContext(ctx context.Context, credential, agent, owner []Grant) context.Context {
	policies := principalCredentialPolicies{
		credential: append([]Grant(nil), credential...),
		agent:      append([]Grant(nil), agent...),
		owner:      append([]Grant(nil), owner...),
	}
	return context.WithValue(ctx, principalCredentialPoliciesContextKey, policies)
}

func workloadPoliciesToContext(ctx context.Context, ceiling, agent []Grant) context.Context {
	policies := workloadPolicies{
		ceiling: append([]Grant(nil), ceiling...),
		agent:   append([]Grant(nil), agent...),
	}
	return context.WithValue(ctx, workloadPoliciesContextKey, policies)
}

func grantAuthorizationFromContext(ctx context.Context) (grantAuthorization, bool) {
	if policies, ok := ctx.Value(principalCredentialPoliciesContextKey).(principalCredentialPolicies); ok {
		return grantAuthorization{policies: [][]Grant{policies.credential, policies.agent, policies.owner}}, true
	}
	if policies, ok := ctx.Value(workloadPoliciesContextKey).(workloadPolicies); ok {
		return grantAuthorization{policies: [][]Grant{policies.ceiling, policies.agent}}, true
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
