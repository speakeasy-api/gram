package authz

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

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

// MandatoryDecision is the outcome of a mandatory check. Allowed is true when
// a policy allowed every check, or DecideServerAdmission or
// DecideAuthClientAdmission explicitly admitted an anonymous caller to a public
// server or to the auth client it resolved. Source distinguishes policy
// evaluation from public visibility admission, which grants only mcp:connect
// and mcp:auth on that server.
type MandatoryDecision struct {
	Allowed bool
	Source  MandatoryPolicySource
	Reason  MandatoryDenyReason
}

var mandatoryScopes = []Scope{ScopeMCPConnect, ScopeMCPAuth}

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
	// publicAnonymous distinguishes absent identity from malformed credential context.
	publicAnonymous bool
	organizationID  string
	caller          MandatoryCaller
	principalID     string
	source          MandatoryPolicySource
	// decided is a decision admission already reached for every check.
	decided       *MandatoryDecision
	authorization grantAuthorization
	key           *apiKeyAdmission
	// narrowedProject is a project a credential minted from a key is bound to.
	narrowedProject *uuid.UUID
	// workloadOwner is resolved by live admission, not credential claims.
	workloadOwner string
	// reportAuthorizerRefusal logs an allowed decision the policies of the
	// user the credential acts for would refuse. Nil when nothing reports.
	reportAuthorizerRefusal func(refused *Check, err error)
}

func (m MandatoryAuthorization) Caller() MandatoryCaller { return m.caller }

// PrincipalID identifies the caller for attribution: a user, API key or
// credential id. It never grants anything.
func (m MandatoryAuthorization) PrincipalID() string { return m.principalID }

func (m MandatoryAuthorization) OrganizationID() string { return m.organizationID }

// RepresentedUserID returns the user the caller represents, if any. It does
// not include the owner bound only for an auth-client admission check.
func (m MandatoryAuthorization) RepresentedUserID() string { return m.authorization.representedUser }

func (m MandatoryAuthorization) authClientRepresentedUserID() string {
	if m.caller == MandatoryCallerWorkload {
		return m.workloadOwner
	}
	return m.RepresentedUserID()
}

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

func (e *Engine) PrepareMandatory(ctx context.Context) (MandatoryAuthorization, error) {
	m := MandatoryAuthorization{
		publicAnonymous:         false,
		organizationID:          "",
		caller:                  MandatoryCallerAnonymous,
		principalID:             "",
		source:                  MandatoryPolicySourceNone,
		decided:                 nil,
		authorization:           grantAuthorization{policies: nil, representedUser: "", representedPolicies: nil, legacyAuthCeiling: false, attached: nil, credentialRemoved: false},
		key:                     nil,
		narrowedProject:         nil,
		workloadOwner:           "",
		reportAuthorizerRefusal: nil,
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		_, hasMode := contextvalues.APIKeyAuthorization(ctx)
		_, hasCredential := contextvalues.PrincipalCredentialAuthorization(ctx)
		_, hasActor := contextvalues.AuthenticatedActor(ctx)
		m.publicAnonymous = authCtx == nil && !hasMode && !hasCredential && !hasActor
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
		ctx = prepared
		authorization, ok = grantAuthorizationFromContext(ctx)
	}
	if !ok {
		return m.decide(MandatoryPolicySourceNone, MandatoryDenyReasonNoPolicy), nil
	}
	m.authorization = authorization
	if m.caller == MandatoryCallerWorkload {
		m.workloadOwner, _ = AdmittedWorkloadOwner(ctx)
	}
	// One request prepares one authorization and may decide many checks with
	// it, so it reports at most one refusal.
	var reported atomic.Bool
	m.reportAuthorizerRefusal = func(refused *Check, err error) {
		if refused == nil && err == nil {
			return
		}
		if reported.Swap(true) {
			return
		}
		reportAuthorizerRefusal(ctx, e.logger, authorization, refused, err)
	}
	return m, nil
}

// WithAttachedConnection applies consent to a connection its owner attached
// to the agent (Engine.AttachedConnectionConsent), replacing any applied
// before, so an admission shared by several decisions carries only the
// current one's. A zero consent applies none. It applies only to an admitted
// agent that acts for a represented user, in the consent's organization; any
// other caller, including a workload with no represented user, gets none.
func (m MandatoryAuthorization) WithAttachedConnection(consent AttachedConnectionConsent) MandatoryAuthorization {
	m.authorization.attached = nil
	if consent.ownerUserID == "" || m.caller != MandatoryCallerAgent || m.authorization.representedUser == "" || consent.organizationID != m.organizationID {
		return m
	}
	m.authorization.attached = &attachedConnection{owner: consent.ownerUserID, policy: append([]Grant{}, consent.ownerPolicy...)}
	return m
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

// MandatoryServerVisibility is the visibility a server is admitted under.
// Anything other than MandatoryServerPublic is decided as private.
type MandatoryServerVisibility string

const (
	MandatoryServerPrivate MandatoryServerVisibility = "private"
	MandatoryServerPublic  MandatoryServerVisibility = "public"
)

// MandatoryPolicySourceVisibility is a public server admitting a caller that
// presents no credential, to the server and to the auth client it resolved.
// It is not a policy and grants nothing else.
const MandatoryPolicySourceVisibility MandatoryPolicySource = "visibility"

// DecideServerAdmission decides mcp:connect on a server in organizationID
// under its visibility. It does no I/O.
//
// A public server admits a caller with no credential: public access needs no
// policy. Every caller that presents a credential is still decided by its
// policy exactly as Decide does, so a credential from another organization is
// never authority here and an inactive or out-of-limits credential stays
// denied. Private servers are decided by Decide alone. An mcp:auth check is
// refused here; DecideAuthClientAdmission decides those.
func (m MandatoryAuthorization) DecideServerAdmission(organizationID string, visibility MandatoryServerVisibility, connect Check) (MandatoryDecision, error) {
	if connect.Scope != ScopeMCPConnect {
		return MandatoryDecision{}, fmt.Errorf("server admission decides %s, not %s", ScopeMCPConnect, connect.Scope)
	}
	if decision, done, err := validateMandatoryChecks([]Check{connect}); done || err != nil {
		return decision, err
	}
	if m.admittedByVisibility(visibility) {
		return visibilityAdmission(), nil
	}
	return m.Decide(organizationID, connect)
}

// DecideAuthClientAdmission decides mcp:auth for using an auth client a server
// in organizationID resolved, under the server's visibility. It does no I/O.
//
// A public server admits a caller with no credential to the client it
// resolved for that caller, as it admits the caller to the server: an
// anonymous caller can reach only what public access already serves it, such
// as the personal connection it made itself. Such a caller names nobody a
// check could carry, so its admission does not read the check. Every caller
// that presents a credential, and every caller of a private server, is
// decided by Decide.
func (m MandatoryAuthorization) DecideAuthClientAdmission(organizationID string, visibility MandatoryServerVisibility, use Check) (MandatoryDecision, error) {
	if use.Scope != ScopeMCPAuth {
		return MandatoryDecision{}, fmt.Errorf("auth client admission decides %s, not %s", ScopeMCPAuth, use.Scope)
	}
	if m.admittedByVisibility(visibility) {
		return visibilityAdmission(), nil
	}
	if m.caller == MandatoryCallerWorkload {
		// Bind the admitted workload owner only for this check; the
		// credential ceiling, assigned agent and owner policies still apply.
		m.authorization.representedUser = m.authClientRepresentedUserID()
	}
	return m.Decide(organizationID, use)
}

// admittedByVisibility reports whether visibility alone admits the caller:
// an anonymous caller that presented no credential, on a public server.
func (m MandatoryAuthorization) admittedByVisibility(visibility MandatoryServerVisibility) bool {
	return visibility == MandatoryServerPublic && m.publicAnonymous && m.caller == MandatoryCallerAnonymous
}

func visibilityAdmission() MandatoryDecision {
	return MandatoryDecision{Allowed: true, Source: MandatoryPolicySourceVisibility, Reason: MandatoryDenyReasonNone}
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
	decision, err := evaluateMandatory(m.authorization, MandatoryPolicySourceRequest, checks)
	if err == nil && decision.Allowed && m.reportAuthorizerRefusal != nil {
		m.reportAuthorizerRefusal(m.authorization.authorizerRefusesEach(checks))
	}
	return decision, err
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
