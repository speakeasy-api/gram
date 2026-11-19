package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
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
	// consent is the owner's consent to the attachment, with their live
	// policy, read when the owner is not the user the agent acts for. The
	// mandatory decision then lets the agent use the owner's connection.
	consent authz.AttachedConnectionConsent
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
//
// An attachment is its owner's consent: the lookup serves only a live
// binding the connection's owner made, to an active agent, while the owner is
// still a member. So when the owner is not the user an agent key acts for,
// their consent and live policy are read too, and the mandatory decision lets
// the agent use that connection whoever issued the key, as long as the
// owner's policy, the key issuer's and the agent's all allow it.
func (s *Service) authClientConnection(ctx context.Context, permit authClientPermitRequest) (authClientConnectionOwner, error) {
	none := authClientConnectionOwner{owner: "", attachedSession: uuid.Nil, authorizer: false, consent: authz.AttachedConnectionConsent{}}
	if permit.ClientType != authClientTypeRemoteSession {
		return none, nil
	}
	if permit.Subject.Kind == urn.SessionSubjectKindUser {
		return authClientConnectionOwner{owner: permit.Subject.ID, attachedSession: uuid.Nil, authorizer: false, consent: authz.AttachedConnectionConsent{}}, nil
	}
	// An agent key with no attachment for this registration is served by its
	// validated authorizer's own connection; the mcp:auth check names that
	// user as the owner and still decides with the agent's own grants.
	// Without a usable attachment the owner is this fallback: the agent
	// key's authorizer, or no owner for any other caller, which no personal
	// grant matches.
	fallback := none
	authorizer, hasAuthorizer := agentKeyAuthorizer(ctx, permit.Subject)
	if hasAuthorizer {
		fallback = authClientConnectionOwner{owner: authorizer, attachedSession: uuid.Nil, authorizer: true, consent: authz.AttachedConnectionConsent{}}
	}
	lookup := s.attachedConnectionLookup()
	if lookup == nil || !permit.RegistrationID.Valid || permit.Subject.IsZero() {
		return fallback, nil
	}
	conn, attached, err := lookup.AuthClientAttachedConnection(ctx, remotesessions.AuthClientTokenRequest{
		ProjectID:           permit.ProjectID,
		OrganizationID:      permit.OrganizationID,
		UserSessionIssuerID: permit.UserSessionIssuerID,
		Subject:             permit.Subject,
		RegistrationID:      permit.RegistrationID.UUID,
		SkipUnusable:        false,
		Attribution:         remotesessions.RefreshAttribution{},
	})
	switch {
	case errors.Is(err, remotesessions.ErrInvalidAuthorizationRequest):
		// A caller the attachment lookup refuses has no attached connection.
		return fallback, nil
	case err != nil:
		return none, fmt.Errorf("resolve attached connection owner: %w", err)
	case !attached:
		return fallback, nil
	case conn.RemoteSessionID == uuid.Nil:
		// A zero connection would disable the pin that binds tokens to
		// the checked connection, so it is never an owner.
		return none, errors.New("resolve attached connection owner: attachment names no connection")
	}
	attachment := authClientConnectionOwner{owner: conn.OwnerUserID, attachedSession: conn.RemoteSessionID, authorizer: false, consent: authz.AttachedConnectionConsent{}}
	// Consent matters only to an agent acting for a user other than the
	// owner; without an engine the decision is made as before.
	if s.authz == nil || !hasAuthorizer || authorizer == conn.OwnerUserID {
		return attachment, nil
	}
	load, required, err := s.attachedConsentUse(ctx, permit.OrganizationID)
	if err != nil {
		return none, fmt.Errorf("resolve attached connection consent use: %w", err)
	}
	if !load {
		return attachment, nil
	}
	consent, err := s.authz.AttachedConnectionConsent(ctx, permit.OrganizationID, conn.OwnerUserID)
	switch {
	case err != nil && required:
		return none, fmt.Errorf("read attached connection consent: %w", err)
	case err != nil:
		// Only the shadow would read it, so the request it observes goes on.
		s.logger.WarnContext(ctx, "attached connection consent unavailable to the mandatory shadow", attr.SlogError(err))
		return attachment, nil
	}
	attachment.consent = consent
	return attachment, nil
}

// attachedConsentUse reports whether a mandatory decision will read an
// attached connection's consent for organizationID: enforcement decides with
// it, so a failed read must refuse (required), and the shadow records the
// decision enforcement would make. Otherwise the connect-equivalent rule
// decides, and the owner's policy is not read at all.
func (s *Service) attachedConsentUse(ctx context.Context, organizationID string) (load, required bool, err error) {
	permitter, err := s.authClientPermitFor(ctx, organizationID)
	if err != nil {
		return false, false, err
	}
	if _, enforced := permitter.(mandatoryPermitter); enforced {
		return true, true, nil
	}
	return feature.MandatoryAuthzShadow(ctx, s.features, organizationID), false, nil
}

// agentKeyAuthorizer is the validated authorizer of an admitted agent
// principal (a principal-backed API key or an agent session) whose agent is
// subject. Signed principal credentials resolve their authorizer earlier
// (connectionSubject); legacy keys and workloads have none.
func agentKeyAuthorizer(ctx context.Context, subject urn.SessionSubject) (string, bool) {
	if subject.Kind != urn.SessionSubjectKindAgent {
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

// tokenSubject is whose connection serves a typed client's tokens for
// subject: the authorizer an unattached agent key's permit was decided for,
// otherwise the connection subject.
func tokenSubject(ctx context.Context, subject urn.SessionSubject, authorizerOwner string) urn.SessionSubject {
	if authorizerOwner != "" {
		return urn.NewUserSubject(authorizerOwner)
	}
	return connectionSubject(ctx, subject)
}

// handbackSubject is whose connection serves a handback to the upstream
// client choice: the authorizer an unattached agent key's permit was decided
// for, otherwise subject unchanged.
func handbackSubject(subject urn.SessionSubject, authorizerOwner string) urn.SessionSubject {
	if authorizerOwner != "" {
		return urn.NewUserSubject(authorizerOwner)
	}
	return subject
}

// upstreamTokenSource resolves a caller's upstream tokens for serving.
type upstreamTokenSource interface {
	ResolveAuthClientAccessTokens(ctx context.Context, req remotesessions.AuthClientTokenRequest) (map[uuid.UUID]remotesessions.UpstreamToken, error)
	ResolveClientAccessTokens(ctx context.Context, attribution remotesessions.RefreshAttribution, projectID uuid.UUID, organizationID string, userSessionIssuerID uuid.UUID, subject urn.SessionSubject, remoteSessionClientID uuid.UUID, skipUnusable bool) (map[uuid.UUID]remotesessions.UpstreamToken, error)
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
