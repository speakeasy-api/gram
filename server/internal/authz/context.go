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
	// representedUser binds $represented_user in every policy, so an agent
	// owner's grants resolve to the user the agent acts for.
	representedUser string
	// representedPolicy is the represented user's own live policy when that
	// user is not the caller. Auth-client checks must also pass it.
	representedPolicy []Grant
}

// RepresentedUserPolicy is the trusted represented-user binding of an admission.
type RepresentedUserPolicy struct {
	UserID string
	Policy []Grant
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

func admittedPoliciesToContext(ctx context.Context, represented RepresentedUserPolicy, sets ...[]Grant) context.Context {
	policies := grantAuthorization{
		policies:          make([][]Grant, 0, len(sets)),
		representedUser:   represented.UserID,
		representedPolicy: nil,
	}
	if represented.UserID != "" {
		policies.representedPolicy = append([]Grant{}, represented.Policy...)
	}
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
		return grantAuthorization{policies: nil, representedUser: "", representedPolicy: nil}, false
	}
	if mode, ok := contextvalues.APIKeyAuthorization(ctx); ok && mode == contextvalues.APIKeyAuthorizationModePrincipal {
		return grantAuthorization{policies: nil, representedUser: "", representedPolicy: nil}, false
	}
	grants, ok := GrantsFromContext(ctx)
	if !ok {
		return grantAuthorization{policies: nil, representedUser: "", representedPolicy: nil}, false
	}
	return grantAuthorization{policies: [][]Grant{grants}, representedUser: loadedGrantsUser(ctx), representedPolicy: nil}, true
}

// loadedGrantsUser is the user whose own grants PrepareContext loaded. API
// keys, assistant tokens and embedded chat sessions represent no user: their
// owner or minter is attribution.
func loadedGrantsUser(ctx context.Context) string {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.APIKeyID != "" || authCtx.ExternalUserID != "" {
		return ""
	}
	if _, assistant := contextvalues.GetAssistantPrincipal(ctx); assistant {
		return ""
	}
	return authCtx.UserID
}

func loadedGrantAuthorization(grants []Grant) grantAuthorization {
	return grantAuthorization{policies: [][]Grant{grants}, representedUser: "", representedPolicy: nil}
}

// bind applies the trusted represented-user binding to an auth-client check,
// replacing any binding the caller set.
func (a grantAuthorization) bind(check Check) Check {
	if check.isAuthClientCheck() {
		check.representedUser = a.representedUser
	}
	return check
}

// policiesFor returns every policy check must pass. An agent acting for a user
// may use an auth client only if that user may use it too.
func (a grantAuthorization) policiesFor(check Check) [][]Grant {
	if a.representedPolicy == nil || !check.isAuthClientCheck() {
		return a.policies
	}
	return append(append([][]Grant{}, a.policies...), a.representedPolicy)
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

	check = a.bind(check)
	allowed := true
	denied := false
	var representative grantCheckEvaluation
	for _, policy := range a.policiesFor(check) {
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
