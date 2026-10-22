package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// MandatoryPolicySource names the policy a mandatory decision evaluated.
type MandatoryPolicySource string

const (
	// MandatoryPolicySourceRequest is the policy the request already holds:
	// a signed-in user, an admitted agent credential or workload, an
	// assistant token, or Platform MCP.
	MandatoryPolicySourceRequest MandatoryPolicySource = "request"
	// MandatoryPolicySourceAPIKey is a legacy API key's own api_key:<id> grants.
	MandatoryPolicySourceAPIKey MandatoryPolicySource = "api_key"
	// MandatoryPolicySourceNone means the caller has no policy to evaluate.
	MandatoryPolicySourceNone MandatoryPolicySource = "none"
)

// MandatoryDenyReason says why a mandatory decision denied.
type MandatoryDenyReason string

const (
	MandatoryDenyReasonNone             MandatoryDenyReason = ""
	MandatoryDenyReasonNoPolicy         MandatoryDenyReason = "no_policy"
	MandatoryDenyReasonAPIKeyInactive   MandatoryDenyReason = "api_key_inactive"
	MandatoryDenyReasonMissingProject   MandatoryDenyReason = "missing_project"
	MandatoryDenyReasonUnsupportedScope MandatoryDenyReason = "unsupported_scope"
	// MandatoryDenyReasonOutsideKey is a check a key's own project binding or
	// transport scopes rule out, whatever its policy says.
	MandatoryDenyReasonOutsideKey MandatoryDenyReason = "outside_key_limits"
	// MandatoryDenyReasonInactiveCredential is a credential admission refused.
	MandatoryDenyReasonInactiveCredential MandatoryDenyReason = "inactive_credential" //nolint:gosec // a reason name, not a credential
	MandatoryDenyReasonNotGranted         MandatoryDenyReason = "not_granted"
	MandatoryDenyReasonBlocked            MandatoryDenyReason = "blocked"
	MandatoryDenyReasonUnauthenticated    MandatoryDenyReason = "unauthenticated"
)

// MandatoryDecision is the outcome of a mandatory check. Allowed is true only
// when a real policy was evaluated and allowed every check.
type MandatoryDecision struct {
	Allowed bool
	Source  MandatoryPolicySource
	Reason  MandatoryDenyReason
}

var mandatoryScopes = []Scope{ScopeMCPConnect, ScopeMCPAuth}

// AuthorizeMandatory evaluates server access and auth client checks for any
// authenticated caller, including those Require skips, such as legacy API keys
// and sessionless callers. It never reads a skipped evaluation as an allow: a
// caller with no policy is denied. Legacy API keys are evaluated against their
// own api_key:<id> grants, never their creator's. Errors are infrastructure
// failures, not decisions.
func (e *Engine) AuthorizeMandatory(ctx context.Context, checks ...Check) (MandatoryDecision, error) {
	if len(checks) == 0 {
		return MandatoryDecision{}, ErrNoChecks
	}
	for _, check := range checks {
		if !slices.Contains(mandatoryScopes, check.Scope) {
			return denyMandatory(MandatoryPolicySourceNone, MandatoryDenyReasonUnsupportedScope), nil
		}
		if err := validateInput(check); err != nil {
			return MandatoryDecision{}, err
		}
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return denyMandatory(MandatoryPolicySourceNone, MandatoryDenyReasonUnauthenticated), nil
	}

	mode, hasMode := contextvalues.APIKeyAuthorization(ctx)
	if hasMode && mode == contextvalues.APIKeyAuthorizationModeLegacy {
		return e.authorizeAPIKey(ctx, authCtx, checks)
	}
	if authCtx.APIKeyID != "" && !hasMode {
		return MandatoryDecision{}, errors.New("api key request without an authorization mode")
	}

	authorization, ok := grantAuthorizationFromContext(ctx)
	if !ok {
		prepared, err := e.PrepareContext(ctx)
		var shareable *oops.ShareableError
		if errors.As(err, &shareable) && (shareable.Code == oops.CodeUnauthorized || shareable.Code == oops.CodeForbidden) {
			return denyMandatory(MandatoryPolicySourceRequest, MandatoryDenyReasonInactiveCredential), nil
		}
		if err != nil {
			return MandatoryDecision{}, fmt.Errorf("prepare mandatory authorization: %w", err)
		}
		authorization, ok = grantAuthorizationFromContext(prepared)
	}
	if !ok {
		return denyMandatory(MandatoryPolicySourceNone, MandatoryDenyReasonNoPolicy), nil
	}
	return evaluateMandatory(authorization, MandatoryPolicySourceRequest, checks)
}

func (e *Engine) authorizeAPIKey(ctx context.Context, authCtx *contextvalues.AuthContext, checks []Check) (MandatoryDecision, error) {
	organizationID := authCtx.ActiveOrganizationID
	keyID, err := uuid.Parse(authCtx.APIKeyID)
	if err != nil {
		return denyMandatory(MandatoryPolicySourceAPIKey, MandatoryDenyReasonAPIKeyInactive), nil
	}
	key, err := admitAPIKey(ctx, e.db, organizationID, keyID)
	if errors.Is(err, ErrPrincipalNotFound) {
		return denyMandatory(MandatoryPolicySourceAPIKey, MandatoryDenyReasonAPIKeyInactive), nil
	}
	if err != nil {
		return MandatoryDecision{}, err
	}
	if !key.reachesMCP() {
		return denyMandatory(MandatoryPolicySourceAPIKey, MandatoryDenyReasonOutsideKey), nil
	}
	for _, check := range checks {
		project := check.Dimensions[SelectorKeyProjectID]
		// A key's server grants are scoped by project, so its checks must
		// say which project the server is in.
		if project == "" {
			return denyMandatory(MandatoryPolicySourceAPIKey, MandatoryDenyReasonMissingProject), nil
		}
		if key.ProjectID.Valid && project != key.ProjectID.UUID.String() {
			return denyMandatory(MandatoryPolicySourceAPIKey, MandatoryDenyReasonOutsideKey), nil
		}
		// A credential minted from the key, such as a chat token, can narrow it
		// to one project.
		if authCtx.ProjectID != nil && project != authCtx.ProjectID.String() {
			return denyMandatory(MandatoryPolicySourceAPIKey, MandatoryDenyReasonOutsideKey), nil
		}
	}
	if len(key.Grants) == 0 {
		return denyMandatory(MandatoryPolicySourceAPIKey, MandatoryDenyReasonNoPolicy), nil
	}
	return evaluateMandatory(loadedGrantAuthorization(key.Grants), MandatoryPolicySourceAPIKey, checks)
}

func evaluateMandatory(authorization grantAuthorization, source MandatoryPolicySource, checks []Check) (MandatoryDecision, error) {
	if authorization.grantCount() == 0 {
		return denyMandatory(source, MandatoryDenyReasonNoPolicy), nil
	}
	for _, check := range checks {
		evaluation, err := authorization.evaluate(check)
		if err != nil {
			return MandatoryDecision{}, fmt.Errorf("evaluate mandatory check: %w", err)
		}
		if evaluation.Grant != nil {
			continue
		}
		if evaluation.Denied {
			return denyMandatory(source, MandatoryDenyReasonBlocked), nil
		}
		return denyMandatory(source, MandatoryDenyReasonNotGranted), nil
	}
	return MandatoryDecision{Allowed: true, Source: source, Reason: MandatoryDenyReasonNone}, nil
}

func denyMandatory(source MandatoryPolicySource, reason MandatoryDenyReason) MandatoryDecision {
	return MandatoryDecision{Allowed: false, Source: source, Reason: reason}
}
