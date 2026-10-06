package remotesessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// resolveCallerUpstreamToken separates admission identity from credential
// source. Only scoped callers can use attachments; a missing or revoked
// attachment never falls back to the agent's, owner's, or authorizer's grant.
func (m *ChallengeManager) resolveCallerUpstreamToken(ctx context.Context, projectID uuid.UUID, organizationID string, userSessionIssuerID, clientID uuid.UUID, caller urn.SessionSubject, resource string) (resolvedUpstreamToken, error) {
	var zero resolvedUpstreamToken
	if invoker, ok := contextvalues.AssistantInvoker(ctx); ok {
		return m.resolveInvokerUpstreamToken(ctx, projectID, organizationID, userSessionIssuerID, clientID, caller, resource, invoker)
	}
	principalID, attached, err := remoteSessionCallerPrincipal(ctx, caller)
	if err != nil {
		return zero, err
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

// resolveInvokerUpstreamToken serves an agent-backed assistant turn with the
// turn user's own consented session for the upstream resource the request was
// authorized for. It never falls back to another user's or the agent's
// session.
func (m *ChallengeManager) resolveInvokerUpstreamToken(ctx context.Context, projectID uuid.UUID, organizationID string, userSessionIssuerID, clientID uuid.UUID, caller urn.SessionSubject, resource, invoker string) (resolvedUpstreamToken, error) {
	var zero resolvedUpstreamToken
	if caller.Kind != urn.SessionSubjectKindUser || caller.ID != invoker {
		return zero, ErrInvalidAuthorizationRequest
	}
	pinned, ok := contextvalues.AssistantBusinessResource(ctx)
	if !ok || (resource != "" && resource != pinned) {
		return zero, nil
	}
	selected, err := remotesessions_repo.New(m.db).GetDelegatedRemoteSession(ctx, remotesessions_repo.GetDelegatedRemoteSessionParams{SubjectUrn: caller, RemoteSessionClientID: clientID, ProjectID: projectID, OrganizationID: organizationID, UserSessionIssuerID: userSessionIssuerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, nil
	}
	if err != nil {
		return zero, fmt.Errorf("select turn user credential: %w", err)
	}
	// A session without a recorded resource cannot prove it targets this one.
	if !selected.Resource.Valid || selected.Resource.String != pinned {
		return zero, nil
	}
	resolved, err := m.resolveCredentialToken(ctx, selected, pinned)
	if err != nil || resolved.Token == "" {
		return resolved, err
	}
	m.touchResolvedCredential(ctx, selected)
	return resolved, nil
}
