package authz

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

type contextKey string

type admissionBoundaryKey struct{}

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
	// representedPolicies are the represented user's own policies when that
	// user is not the caller: the live policy, plus the admitted one under an
	// admission boundary. Auth-client checks must pass every one.
	representedPolicies [][]Grant
	// legacyAuthCeiling marks a stored policy older than version 5, whose
	// auth client ceiling admission derived from its server access.
	legacyAuthCeiling bool
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

// RefreshContext reloads authorization for a new operation within a long-lived
// request. Immutable credential constraints remain attached to the context.
func (e *Engine) RefreshContext(ctx context.Context) (context.Context, error) {
	ctx = context.WithValue(ctx, grantsContextKey, struct{}{})
	ctx = context.WithValue(ctx, admittedPoliciesContextKey, struct{}{})
	return e.PrepareContext(ctx)
}

// AdmissionBoundary retains only authorization policies for an admitted job.
// A newly granted permission cannot expand the running job's authority.
type AdmissionBoundary struct {
	policies grantAuthorization
	valid    bool
}

// CaptureAdmissionBoundary captures prepared policies without retaining a request.
func (e *Engine) CaptureAdmissionBoundary(ctx context.Context) (AdmissionBoundary, error) {
	policies, valid := grantAuthorizationFromContext(ctx)
	if !valid {
		enforce, err := e.ShouldEnforce(ctx)
		if err != nil {
			return AdmissionBoundary{policies: policies, valid: false}, fmt.Errorf("check admission enforcement: %w", err)
		}
		if enforce {
			return AdmissionBoundary{policies: policies, valid: false}, fmt.Errorf("admission requires prepared authorization")
		}
	}
	return AdmissionBoundary{policies: policies, valid: valid}, nil
}

// Apply intersects all later grant refreshes with the original admission.
func (b AdmissionBoundary) Apply(ctx context.Context) context.Context {
	if !b.valid {
		return ctx
	}
	return context.WithValue(ctx, admissionBoundaryKey{}, b.policies)
}

func admittedPoliciesToContext(ctx context.Context, represented RepresentedUserPolicy, legacyAuthCeiling bool, sets ...[]Grant) context.Context {
	policies := grantAuthorization{
		policies:            make([][]Grant, 0, len(sets)),
		representedUser:     represented.UserID,
		representedPolicies: nil,
		legacyAuthCeiling:   legacyAuthCeiling,
	}
	if represented.UserID != "" {
		policies.representedPolicies = [][]Grant{append([]Grant{}, represented.Policy...)}
	}
	for _, set := range sets {
		policies.policies = append(policies.policies, append([]Grant(nil), set...))
	}
	return context.WithValue(ctx, admittedPoliciesContextKey, policies)
}

func grantAuthorizationFromContext(ctx context.Context) (grantAuthorization, bool) {
	current, ok := currentGrantAuthorizationFromContext(ctx)
	if !ok {
		return current, false
	}
	if boundary, bounded := ctx.Value(admissionBoundaryKey{}).(grantAuthorization); bounded {
		policies := make([][]Grant, 0, len(current.policies)+len(boundary.policies))
		policies = append(policies, current.policies...)
		policies = append(policies, boundary.policies...)
		representedPolicies := make([][]Grant, 0, len(current.representedPolicies)+len(boundary.representedPolicies))
		representedPolicies = append(representedPolicies, current.representedPolicies...)
		representedPolicies = append(representedPolicies, boundary.representedPolicies...)
		return grantAuthorization{policies: policies, representedUser: current.representedUser, representedPolicies: representedPolicies, legacyAuthCeiling: current.legacyAuthCeiling || boundary.legacyAuthCeiling}, true
	}
	return current, true
}

func currentGrantAuthorizationFromContext(ctx context.Context) (grantAuthorization, bool) {
	if policies, ok := ctx.Value(admittedPoliciesContextKey).(grantAuthorization); ok {
		return policies, true
	}
	if _, principalCredential := contextvalues.PrincipalCredentialAuthorization(ctx); principalCredential {
		return grantAuthorization{policies: nil, representedUser: "", representedPolicies: nil, legacyAuthCeiling: false}, false
	}
	if mode, ok := contextvalues.APIKeyAuthorization(ctx); ok && mode == contextvalues.APIKeyAuthorizationModePrincipal {
		return grantAuthorization{policies: nil, representedUser: "", representedPolicies: nil, legacyAuthCeiling: false}, false
	}
	grants, ok := GrantsFromContext(ctx)
	if !ok {
		return grantAuthorization{policies: nil, representedUser: "", representedPolicies: nil, legacyAuthCeiling: false}, false
	}
	return grantAuthorization{policies: [][]Grant{grants}, representedUser: loadedGrantsUser(ctx), representedPolicies: nil, legacyAuthCeiling: false}, true
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
	return grantAuthorization{policies: [][]Grant{grants}, representedUser: "", representedPolicies: nil, legacyAuthCeiling: false}
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
	if len(a.representedPolicies) == 0 || !check.isAuthClientCheck() {
		return a.policies
	}
	return append(append([][]Grant{}, a.policies...), a.representedPolicies...)
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
