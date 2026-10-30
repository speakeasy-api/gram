package remotesessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth/principalcredential"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// remoteSessionCallerPrincipal identifies the caller, not the user who owns
// its credential. API keys use the authenticated actor, never the key creator
// or authorizer. Agent session callers also carry an explicit agent subject.
func remoteSessionCallerPrincipal(ctx context.Context, subject urn.SessionSubject) (uuid.UUID, bool, error) {
	actor, authenticated := contextvalues.AuthenticatedActor(ctx)
	if authenticated && actor.Type == urn.PrincipalTypeAgent {
		id, err := uuid.Parse(actor.ID)
		if err != nil || id == uuid.Nil {
			return uuid.Nil, true, ErrInvalidAuthorizationRequest
		}
		if subject.Kind == urn.SessionSubjectKindAgent && subject.ID != actor.ID {
			return uuid.Nil, true, ErrInvalidAuthorizationRequest
		}
		return id, true, nil
	}
	if subject.Kind == urn.SessionSubjectKindAgent {
		if authenticated {
			return uuid.Nil, true, ErrInvalidAuthorizationRequest
		}
		id, err := uuid.Parse(subject.ID)
		if err != nil || id == uuid.Nil {
			return uuid.Nil, true, ErrInvalidAuthorizationRequest
		}
		return id, true, nil
	}
	return uuid.Nil, false, nil
}

// principalAPIKeyAuthorizer reports the validated authorizer of a
// principal-backed API key when caller is exactly that user's subject, the
// owner the typed auth client check named. Any other caller keeps the
// key's own principal.
func principalAPIKeyAuthorizer(ctx context.Context, caller urn.SessionSubject) (string, bool) {
	mode, ok := contextvalues.APIKeyAuthorization(ctx)
	if !ok || mode != contextvalues.APIKeyAuthorizationModePrincipal || caller.Kind != urn.SessionSubjectKindUser {
		return "", false
	}
	credential, ok := contextvalues.PrincipalCredentialAuthorization(ctx)
	if !ok || credential.AuthorizerUserID == "" || credential.AuthorizerUserID != caller.ID {
		return "", false
	}
	return credential.AuthorizerUserID, true
}

// resolveCallerUpstreamToken separates admission identity from credential
// source. Only scoped callers can use attachments; a missing or revoked
// attachment never falls back to the agent's, owner's, or authorizer's grant.
func (m *ChallengeManager) resolveCallerUpstreamToken(ctx context.Context, lookup tokenLookup, projectID uuid.UUID, organizationID string, userSessionIssuerID, clientID uuid.UUID, caller urn.SessionSubject, resource string) (resolvedUpstreamToken, error) {
	var zero resolvedUpstreamToken
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok && actor.Type == urn.PrincipalTypeWorkload {
		return m.resolveWorkloadUpstreamToken(ctx, lookup, projectID, organizationID, userSessionIssuerID, clientID, caller, resource)
	}
	// A subject alone is not authority to select a workload owner's grant.
	if caller.Kind == urn.SessionSubjectKindWorkload {
		return zero, ErrInvalidAuthorizationRequest
	}
	if credential, ok := principalcredential.FromContext(ctx); ok {
		return m.resolveAuthorizerUpstreamToken(ctx, lookup, projectID, organizationID, userSessionIssuerID, clientID, resource, credential.Credential.AuthorizerUserID)
	}
	// A typed auth client already checked the agent's own mcp:auth against
	// the authorizer as the connection's owner; the key then reads exactly
	// that user's connection, never an attachment or the agent's own.
	if authorizer, ok := principalAPIKeyAuthorizer(ctx, caller); ok && lookup == lookupAuthClient {
		return m.resolveAuthorizerUpstreamToken(ctx, lookup, projectID, organizationID, userSessionIssuerID, clientID, resource, authorizer)
	}
	principalID, attached, err := remoteSessionCallerPrincipal(ctx, caller)
	if err != nil {
		return zero, err
	}
	if !attached && lookup == lookupAuthClient {
		return m.resolveTenantUpstreamToken(ctx, projectID, organizationID, clientID, caller, resource)
	}
	if !attached {
		return m.resolveUpstreamToken(ctx, clientID, caller, resource)
	}
	if projectID == uuid.Nil || organizationID == "" || userSessionIssuerID == uuid.Nil || clientID == uuid.Nil {
		return zero, ErrInvalidAuthorizationRequest
	}
	q := remotesessions_repo.New(m.db)
	params := remotesessions_repo.GetPrincipalRemoteSessionBindingParams{
		ProjectID:             projectID,
		OrganizationID:        organizationID,
		PrincipalID:           principalID,
		UserSessionIssuerID:   userSessionIssuerID,
		RemoteSessionClientID: clientID,
	}
	source, err := q.GetPrincipalRemoteSessionBinding(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, nil
	}
	if err != nil {
		return zero, fmt.Errorf("get principal remote session binding: %w", err)
	}
	if source.SubjectUrn.Kind != urn.SessionSubjectKindUser {
		return zero, nil
	}
	resolved, err := m.resolveCredentialToken(ctx, source, resource)
	if err != nil || resolved.Token == "" {
		return resolved, err
	}
	// Refresh can block on an upstream request. Recheck attachment authorization
	// before releasing the token, without adopting a changed credential source.
	current, err := q.GetPrincipalRemoteSessionBinding(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, nil
	}
	if err != nil {
		return zero, fmt.Errorf("recheck principal remote session binding: %w", err)
	}
	if current.ID != source.ID || current.GrantGeneration != source.GrantGeneration {
		return zero, nil
	}
	m.touchResolvedCredential(ctx, source)
	return resolved, nil
}

// resolveTenantUpstreamToken is resolveUpstreamToken for the auth client
// resolver: a plain caller's own session, only when its consent came through
// the server's organization.
func (m *ChallengeManager) resolveTenantUpstreamToken(ctx context.Context, projectID uuid.UUID, organizationID string, clientID uuid.UUID, caller urn.SessionSubject, resource string) (resolvedUpstreamToken, error) {
	var zero resolvedUpstreamToken
	q := remotesessions_repo.New(m.db)
	sess, err := activeUserRemoteSession(ctx, q, lookupAuthClient, caller, clientID, projectID, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return zero, nil
	case err != nil:
		return zero, err
	}
	resolved, err := m.resolveCredentialToken(ctx, sess, resource)
	if err != nil || resolved.Token == "" {
		return resolved, err
	}
	// Recheck after a possibly blocking refresh, without adopting a reconnected grant.
	current, err := activeUserRemoteSession(ctx, q, lookupAuthClient, caller, clientID, projectID, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return zero, nil
	case err != nil:
		return zero, fmt.Errorf("recheck caller credential: %w", err)
	}
	if current.ID != sess.ID || current.GrantGeneration != sess.GrantGeneration {
		return zero, nil
	}
	m.touchResolvedCredential(ctx, sess)
	return resolved, nil
}

// resolveAuthorizerUpstreamToken serves a request made with a principal
// credential from the upstream session of the user the credential acts for,
// on the fly: the session is the user's own, never bound to the credential's
// principal. A credential with no authorizing user has no session to use.
// This path never falls back to an owner or the principal's own sessions.
func (m *ChallengeManager) resolveAuthorizerUpstreamToken(ctx context.Context, lookup tokenLookup, projectID uuid.UUID, organizationID string, userSessionIssuerID, clientID uuid.UUID, resource, authorizerUserID string) (resolvedUpstreamToken, error) {
	var zero resolvedUpstreamToken
	if authorizerUserID == "" {
		return zero, nil
	}
	q := remotesessions_repo.New(m.db)
	authorizer := urn.NewUserSubject(authorizerUserID)
	selected, err := tenantUserRemoteSession(ctx, q, lookup, authorizer, clientID, projectID, organizationID, userSessionIssuerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, nil
	}
	if err != nil {
		return zero, fmt.Errorf("select authorizing user credential: %w", err)
	}
	resolved, err := m.resolveCredentialToken(ctx, selected, resource)
	if err != nil || resolved.Token == "" {
		return resolved, err
	}
	// Recheck after a possibly blocking refresh, without adopting a reconnected grant.
	current, err := tenantUserRemoteSession(ctx, q, lookup, authorizer, clientID, projectID, organizationID, userSessionIssuerID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return zero, nil
	case err != nil:
		return zero, fmt.Errorf("recheck authorizing user credential: %w", err)
	}
	if current.ID != selected.ID || current.GrantGeneration != selected.GrantGeneration {
		return zero, nil
	}
	m.touchResolvedCredential(ctx, selected)
	return resolved, nil
}

// workloadCredentialOwner resolves live assignment and owner eligibility using
// the same admission snapshot as workload authorization. Provenance cached on
// the context is not sufficient after a blocking upstream refresh.
func (m *ChallengeManager) workloadCredentialOwner(ctx context.Context, projectID uuid.UUID, organizationID string, caller urn.SessionSubject) (authz.WorkloadSessionAdmission, error) {
	var zero authz.WorkloadSessionAdmission
	auth, ok := contextvalues.GetAuthContext(ctx)
	actor, authenticated := contextvalues.AuthenticatedActor(ctx)
	if !ok || auth == nil || !authenticated || actor.Type != urn.PrincipalTypeWorkload ||
		caller.Kind != urn.SessionSubjectKindWorkload || caller.ID != actor.ID ||
		organizationID == "" || projectID == uuid.Nil || auth.ActiveOrganizationID != organizationID ||
		(auth.ProjectID != nil && *auth.ProjectID != projectID) {
		return zero, ErrInvalidAuthorizationRequest
	}
	admission, err := runtimepolicy.AdmitWorkloadSession(ctx, m.db)
	if err != nil {
		return zero, fmt.Errorf("admit workload credential owner: %w", err)
	}
	allowed, err := authz.GrantsAuthorize(admission.Owner, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: projectID.String(), ResourceKind: "", Dimensions: nil})
	if err != nil {
		return zero, fmt.Errorf("authorize workload owner project access: %w", err)
	}
	if !allowed {
		return zero, ErrNoValidToken
	}
	return admission, nil
}

func (m *ChallengeManager) resolveWorkloadUpstreamToken(ctx context.Context, lookup tokenLookup, projectID uuid.UUID, organizationID string, userSessionIssuerID, clientID uuid.UUID, caller urn.SessionSubject, resource string) (resolvedUpstreamToken, error) {
	var zero resolvedUpstreamToken
	admission, err := m.workloadCredentialOwner(ctx, projectID, organizationID, caller)
	if err != nil {
		return zero, err
	}
	q := remotesessions_repo.New(m.db)
	owner := urn.NewUserSubject(admission.OwnerUserID)
	selected, err := tenantUserRemoteSession(ctx, q, lookup, owner, clientID, projectID, organizationID, userSessionIssuerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, nil
	}
	if err != nil {
		return zero, fmt.Errorf("select workload owner credential: %w", err)
	}
	resolved, err := m.resolveCredentialToken(ctx, selected, resource)
	if err != nil || resolved.Token == "" {
		return resolved, err
	}
	current, err := m.workloadCredentialOwner(ctx, projectID, organizationID, caller)
	if err != nil {
		return zero, err
	}
	if current.AgentPrincipal != admission.AgentPrincipal || current.OwnerUserID != admission.OwnerUserID {
		return zero, nil
	}
	// Recheck the tenant binding, membership and grant after refresh, without
	// adopting a replacement session or a reconnected grant.
	session, err := tenantUserRemoteSession(ctx, q, lookup, owner, clientID, projectID, organizationID, userSessionIssuerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, nil
	}
	if err != nil {
		return zero, fmt.Errorf("recheck workload owner credential: %w", err)
	}
	if session.ID != selected.ID || session.GrantGeneration != selected.GrantGeneration {
		return zero, nil
	}
	m.touchResolvedCredential(ctx, selected)
	return resolved, nil
}
