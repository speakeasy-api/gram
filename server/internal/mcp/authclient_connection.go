package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// attachedConnectionLookup names the stored connection an attached agent's
// binding serves for one registration.
type attachedConnectionLookup interface {
	AuthClientAttachedConnection(ctx context.Context, req remotesessions.AuthClientTokenRequest) (remotesessions.AttachedConnection, bool, error)
}

// authClientConnectionOwner is whose personal connection a use of a resolved
// client serves.
type authClientConnectionOwner struct {
	// owner is the user who owns the connection, empty when there is none.
	owner string
	// attachedSession is the exact remote session an attachment serves, set
	// only for an attached agent.
	attachedSession uuid.UUID
	// authorizer marks an unattached principal-backed agent key served by its
	// validated authorizer's own connection.
	authorizer bool
}

// authClientConnection resolves the stored owner of the personal connection
// permit's client serves, which the mcp:auth check filters on. A user's own
// connection is theirs. An agent is served only by the exact connection a
// user attached to it, so its owner is that connection's user, read through
// the same tenant- and sign-in-scoped binding lookup token resolution uses,
// never the agent's owner or the admitted subject. The caller's admitted
// identity is unchanged: the owner is a check dimension that only filters
// grants, so an attachment from anyone but the represented user still grants
// nothing. A caller with no usable attachment has no owner, which no personal
// grant matches.
func (s *Service) authClientConnection(ctx context.Context, permit authClientPermitRequest) (authClientConnectionOwner, error) {
	var none authClientConnectionOwner
	if permit.ClientType != authClientTypeRemoteSession {
		return none, nil
	}
	if permit.Subject.Kind == urn.SessionSubjectKindUser {
		return authClientConnectionOwner{owner: permit.Subject.ID, attachedSession: uuid.Nil, authorizer: false}, nil
	}
	// An agent key with no attachment for this registration is served by its
	// validated authorizer's own connection; the mcp:auth check names that
	// user as the owner and still decides with the agent's own grants.
	if authorizer, ok := agentKeyAuthorizer(ctx, permit.Subject); ok {
		none = authClientConnectionOwner{owner: authorizer, attachedSession: uuid.Nil, authorizer: true}
	}
	lookup := s.attachedConnectionLookup()
	if lookup == nil || !permit.RegistrationID.Valid || permit.Subject.IsZero() {
		return none, nil
	}
	conn, attached, err := lookup.AuthClientAttachedConnection(ctx, remotesessions.AuthClientTokenRequest{
		ProjectID:           permit.ProjectID,
		OrganizationID:      permit.OrganizationID,
		UserSessionIssuerID: permit.UserSessionIssuerID,
		Subject:             permit.Subject,
		RegistrationID:      permit.RegistrationID.UUID,
		SkipUnusable:        false,
	})
	switch {
	case errors.Is(err, remotesessions.ErrInvalidAuthorizationRequest):
		// A caller the attachment lookup refuses has no connection to own.
		return none, nil
	case err != nil:
		return none, fmt.Errorf("resolve attached connection owner: %w", err)
	case !attached:
		return none, nil
	case conn.RemoteSessionID == uuid.Nil:
		// A zero connection would disable the pin that binds tokens to
		// the checked connection, so it is never an owner.
		return none, errors.New("resolve attached connection owner: attachment names no connection")
	}
	return authClientConnectionOwner{owner: conn.OwnerUserID, attachedSession: conn.RemoteSessionID, authorizer: false}, nil
}

// agentKeyAuthorizer is the validated authorizer of a principal-backed agent
// API key whose agent is subject. Agent sessions, signed principal
// credentials, legacy keys and workloads have none.
func agentKeyAuthorizer(ctx context.Context, subject urn.SessionSubject) (string, bool) {
	mode, ok := contextvalues.APIKeyAuthorization(ctx)
	if !ok || mode != contextvalues.APIKeyAuthorizationModePrincipal || subject.Kind != urn.SessionSubjectKindAgent {
		return "", false
	}
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	if !ok || actor.Type != urn.PrincipalTypeAgent || actor.ID != subject.ID {
		return "", false
	}
	credential, ok := contextvalues.PrincipalCredentialAuthorization(ctx)
	if !ok || credential.AuthorizerUserID == "" {
		return "", false
	}
	return credential.AuthorizerUserID, true
}

// tokenSubject is whose connection serves tokens for subject: the authorizer
// an unattached agent key's permit was decided for, otherwise the connection
// subject.
func tokenSubject(ctx context.Context, subject urn.SessionSubject, res *authClientResolution) urn.SessionSubject {
	if res != nil && res.authorizerOwner != "" {
		return urn.NewUserSubject(res.authorizerOwner)
	}
	return connectionSubject(ctx, subject)
}

// upstreamTokenSource resolves a caller's upstream tokens for serving.
type upstreamTokenSource interface {
	ResolveAuthClientAccessTokens(ctx context.Context, req remotesessions.AuthClientTokenRequest) (map[uuid.UUID]remotesessions.UpstreamToken, error)
	ResolveClientAccessTokens(ctx context.Context, projectID uuid.UUID, organizationID string, userSessionIssuerID uuid.UUID, subject urn.SessionSubject, remoteSessionClientID uuid.UUID, skipUnusable bool) (map[uuid.UUID]remotesessions.UpstreamToken, error)
}

func (s *Service) upstreamTokenSource() upstreamTokenSource {
	if s.upstreamTokens != nil {
		return s.upstreamTokens
	}
	return s.remoteChallengeMgr
}

func (s *Service) attachedConnectionLookup() attachedConnectionLookup {
	if s.attachedConnections != nil {
		return s.attachedConnections
	}
	if s.remoteChallengeMgr == nil {
		return nil
	}
	return s.remoteChallengeMgr
}

// pinAttachedSession keeps only tokens from the exact connection an attached
// agent's permit was decided for. A binding that moved to another connection
// after the check serves nothing; without skipUnusable that, or no token at
// all, is a missing token, never an anonymous call.
func pinAttachedSession(tokens map[uuid.UUID]remotesessions.UpstreamToken, session uuid.UUID, skipUnusable bool) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	if session == uuid.Nil {
		return tokens, nil
	}
	pinned := make(map[uuid.UUID]remotesessions.UpstreamToken, len(tokens))
	for issuerID, token := range tokens {
		if token.RemoteSessionID == session {
			pinned[issuerID] = token
		}
	}
	if (len(pinned) != len(tokens) || len(pinned) == 0) && !skipUnusable {
		return nil, remotesessions.ErrNoValidToken
	}
	return pinned, nil
}
