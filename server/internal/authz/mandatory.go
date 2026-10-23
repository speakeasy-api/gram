package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

// MandatoryCaller is the kind of caller a mandatory decision evaluated.
type MandatoryCaller string

const (
	MandatoryCallerAnonymous   MandatoryCaller = "anonymous"
	MandatoryCallerUser        MandatoryCaller = "user"
	MandatoryCallerAgent       MandatoryCaller = "agent"
	MandatoryCallerWorkload    MandatoryCaller = "workload"
	MandatoryCallerAPIKey      MandatoryCaller = "api_key"
	MandatoryCallerAssistant   MandatoryCaller = "assistant"
	MandatoryCallerChatSession MandatoryCaller = "chat_session"
)

// MandatoryDenyReasonOrganizationMismatch is a credential from another
// organization: it is never authority in the target organization.
const MandatoryDenyReasonOrganizationMismatch MandatoryDenyReason = "organization_mismatch"

// MandatoryAuthorization is a caller's admitted mandatory policy, prepared
// once per request so several checks share one admission.
type MandatoryAuthorization struct {
	organizationID string
	caller         MandatoryCaller
	principalID    string
	source         MandatoryPolicySource
	// decided is a decision admission already reached for every check.
	decided       *MandatoryDecision
	authorization grantAuthorization
	key           *apiKeyAdmission
	// narrowedProject is a project a credential minted from a key is bound to.
	narrowedProject *uuid.UUID
}

// Caller is the kind of caller evaluated.
func (m MandatoryAuthorization) Caller() MandatoryCaller { return m.caller }

// PrincipalID identifies the caller for attribution: a user, API key or
// credential id. It never grants anything.
func (m MandatoryAuthorization) PrincipalID() string { return m.principalID }

// OrganizationID is the organization the caller authenticated in.
func (m MandatoryAuthorization) OrganizationID() string { return m.organizationID }

// RepresentedUserID is the user bound to $represented_user, the same binding
// enforcement uses. Callers that represent no user return "".
func (m MandatoryAuthorization) RepresentedUserID() string { return m.authorization.representedUser }

// LegacyAuthCeiling reports whether admission derived the caller's auth
// client ceiling from server access in a pre-version-5 stored policy.
func (m MandatoryAuthorization) LegacyAuthCeiling() bool { return m.authorization.legacyAuthCeiling }

// AuthorizeMandatory evaluates server access and auth client checks for any
// authenticated caller in its own organization, including those Require
// skips, such as legacy API keys and sessionless callers. It never reads a
// skipped evaluation as an allow: a caller with no policy is denied. Legacy
// API keys are evaluated against their own api_key:<id> grants, never their
// creator's. Errors are infrastructure failures, not decisions.
func (e *Engine) AuthorizeMandatory(ctx context.Context, checks ...Check) (MandatoryDecision, error) {
	if decision, done, err := validateMandatoryChecks(checks); done || err != nil {
		return decision, err
	}
	authorization, err := e.PrepareMandatory(ctx)
	if err != nil {
		return MandatoryDecision{}, err
	}
	return authorization.Decide(authorization.organizationID, checks...)
}

// PrepareMandatory admits the caller once for mandatory decisions. It does not
// change ctx.
func (e *Engine) PrepareMandatory(ctx context.Context) (MandatoryAuthorization, error) {
	m := MandatoryAuthorization{
		organizationID:  "",
		caller:          MandatoryCallerAnonymous,
		principalID:     "",
		source:          MandatoryPolicySourceNone,
		decided:         nil,
		authorization:   grantAuthorization{policies: nil, representedUser: "", representedPolicy: nil, legacyAuthCeiling: false},
		key:             nil,
		narrowedProject: nil,
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return m.decide(MandatoryPolicySourceNone, MandatoryDenyReasonUnauthenticated), nil
	}
	m.organizationID = authCtx.ActiveOrganizationID
	m.caller, m.principalID = mandatoryCaller(ctx, authCtx)

	mode, hasMode := contextvalues.APIKeyAuthorization(ctx)
	if hasMode && mode == contextvalues.APIKeyAuthorizationModeLegacy {
		return e.prepareAPIKey(ctx, m, authCtx)
	}
	if authCtx.APIKeyID != "" && !hasMode {
		return m, errors.New("api key request without an authorization mode")
	}

	m.source = MandatoryPolicySourceRequest
	if m.caller == MandatoryCallerUser {
		// Kept when admission denies, so the decision still names the user.
		m.authorization.representedUser = loadedGrantsUser(ctx)
	}
	authorization, ok := grantAuthorizationFromContext(ctx)
	if !ok {
		prepared, err := e.PrepareContext(ctx)
		var shareable *oops.ShareableError
		if errors.As(err, &shareable) && (shareable.Code == oops.CodeUnauthorized || shareable.Code == oops.CodeForbidden) {
			return m.decide(MandatoryPolicySourceRequest, MandatoryDenyReasonInactiveCredential), nil
		}
		if err != nil {
			return m, fmt.Errorf("prepare mandatory authorization: %w", err)
		}
		authorization, ok = grantAuthorizationFromContext(prepared)
	}
	if !ok {
		return m.decide(MandatoryPolicySourceNone, MandatoryDenyReasonNoPolicy), nil
	}
	m.authorization = authorization
	return m, nil
}

func (e *Engine) prepareAPIKey(ctx context.Context, m MandatoryAuthorization, authCtx *contextvalues.AuthContext) (MandatoryAuthorization, error) {
	m.source = MandatoryPolicySourceAPIKey
	keyID, err := uuid.Parse(authCtx.APIKeyID)
	if err != nil {
		return m.decide(MandatoryPolicySourceAPIKey, MandatoryDenyReasonAPIKeyInactive), nil
	}
	key, err := admitAPIKey(ctx, e.db, m.organizationID, keyID)
	if errors.Is(err, ErrPrincipalNotFound) {
		return m.decide(MandatoryPolicySourceAPIKey, MandatoryDenyReasonAPIKeyInactive), nil
	}
	if err != nil {
		return m, err
	}
	if !key.reachesMCP() {
		return m.decide(MandatoryPolicySourceAPIKey, MandatoryDenyReasonOutsideKey), nil
	}
	m.key = &key
	m.narrowedProject = authCtx.ProjectID
	m.authorization = loadedGrantAuthorization(key.Grants)
	return m, nil
}

func (m MandatoryAuthorization) decide(source MandatoryPolicySource, reason MandatoryDenyReason) MandatoryAuthorization {
	decision := denyMandatory(source, reason)
	m.decided = &decision
	return m
}

// Decide evaluates checks on a server in organizationID. It does no I/O.
func (m MandatoryAuthorization) Decide(organizationID string, checks ...Check) (MandatoryDecision, error) {
	if decision, done, err := validateMandatoryChecks(checks); done || err != nil {
		return decision, err
	}
	if m.caller == MandatoryCallerAnonymous {
		return denyMandatory(MandatoryPolicySourceNone, MandatoryDenyReasonUnauthenticated), nil
	}
	if organizationID != m.organizationID {
		return denyMandatory(m.source, MandatoryDenyReasonOrganizationMismatch), nil
	}
	if m.decided != nil {
		return *m.decided, nil
	}
	if m.key != nil {
		return m.decideAPIKey(checks)
	}
	return evaluateMandatory(m.authorization, MandatoryPolicySourceRequest, checks)
}

// validateMandatoryChecks rejects malformed checks before any admission. done
// is a decision reached without evaluating a policy.
func validateMandatoryChecks(checks []Check) (decision MandatoryDecision, done bool, err error) {
	if len(checks) == 0 {
		return MandatoryDecision{}, false, ErrNoChecks
	}
	for _, check := range checks {
		if !slices.Contains(mandatoryScopes, check.Scope) {
			return denyMandatory(MandatoryPolicySourceNone, MandatoryDenyReasonUnsupportedScope), true, nil
		}
		if err := validateInput(check); err != nil {
			return MandatoryDecision{}, false, err
		}
	}
	return MandatoryDecision{Allowed: false, Source: MandatoryPolicySourceNone, Reason: MandatoryDenyReasonNone}, false, nil
}

func (m MandatoryAuthorization) decideAPIKey(checks []Check) (MandatoryDecision, error) {
	for _, check := range checks {
		project := check.Dimensions[SelectorKeyProjectID]
		// A key's server grants are scoped by project, so its checks must
		// say which project the server is in.
		if project == "" {
			return denyMandatory(MandatoryPolicySourceAPIKey, MandatoryDenyReasonMissingProject), nil
		}
		if m.key.ProjectID.Valid && project != m.key.ProjectID.UUID.String() {
			return denyMandatory(MandatoryPolicySourceAPIKey, MandatoryDenyReasonOutsideKey), nil
		}
		// A credential minted from the key, such as a chat token, can narrow it
		// to one project.
		if m.narrowedProject != nil && project != m.narrowedProject.String() {
			return denyMandatory(MandatoryPolicySourceAPIKey, MandatoryDenyReasonOutsideKey), nil
		}
	}
	if len(m.key.Grants) == 0 {
		return denyMandatory(MandatoryPolicySourceAPIKey, MandatoryDenyReasonNoPolicy), nil
	}
	return evaluateMandatory(m.authorization, MandatoryPolicySourceAPIKey, checks)
}

// mandatoryCaller classifies an authenticated caller for attribution.
func mandatoryCaller(ctx context.Context, authCtx *contextvalues.AuthContext) (MandatoryCaller, string) {
	if mode, ok := contextvalues.APIKeyAuthorization(ctx); ok {
		if mode == contextvalues.APIKeyAuthorizationModeLegacy {
			return MandatoryCallerAPIKey, authCtx.APIKeyID
		}
		return MandatoryCallerAgent, authCtx.APIKeyID
	}
	if authCtx.APIKeyID != "" {
		return MandatoryCallerAPIKey, authCtx.APIKeyID
	}
	actor, hasActor := contextvalues.AuthenticatedActor(ctx)
	if hasActor && actor.Type == urn.PrincipalTypeWorkload {
		return MandatoryCallerWorkload, actor.ID
	}
	if _, ok := contextvalues.PrincipalCredentialAuthorization(ctx); ok {
		if hasActor {
			return MandatoryCallerAgent, actor.ID
		}
		return MandatoryCallerAgent, ""
	}
	if assistant, ok := contextvalues.GetAssistantPrincipal(ctx); ok {
		return MandatoryCallerAssistant, assistant.AssistantID.String()
	}
	if authCtx.ExternalUserID != "" {
		return MandatoryCallerChatSession, authCtx.UserID
	}
	return MandatoryCallerUser, authCtx.UserID
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
