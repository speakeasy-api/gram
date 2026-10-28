package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

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
		return authClientConnectionOwner{owner: permit.Subject.ID, attachedSession: uuid.Nil}, nil
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
	}
	return authClientConnectionOwner{owner: conn.OwnerUserID, attachedSession: conn.RemoteSessionID}, nil
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
// after the check serves nothing; without skipUnusable that is a missing
// token, never an anonymous call.
func pinAttachedSession(tokens map[uuid.UUID]remotesessions.UpstreamToken, res *authClientResolution, skipUnusable bool) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	if res == nil || res.attachedSession == uuid.Nil {
		return tokens, nil
	}
	pinned := make(map[uuid.UUID]remotesessions.UpstreamToken, len(tokens))
	for issuerID, token := range tokens {
		if token.RemoteSessionID == res.attachedSession {
			pinned[issuerID] = token
		}
	}
	if len(pinned) != len(tokens) && !skipUnusable {
		return nil, remotesessions.ErrNoValidToken
	}
	return pinned, nil
}
