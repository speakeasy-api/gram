package mcp

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// CodeReconnectBinding binds an upstream sign-in to the user and member that
// requested it through code mode. It does not approve a new tool catalogue.
type CodeReconnectBinding struct {
	UserID    string    `json:"user_id"`
	Server    string    `json:"server"`
	ServerID  uuid.UUID `json:"server_id"`
	ExpiresAt time.Time `json:"expires_at"`
	Ticket    bool      `json:"ticket,omitempty"`
}

func (b *CodeReconnectBinding) authorize(userID string, impersonated bool, now time.Time) error {
	if b == nil {
		return nil
	}
	if b.Ticket || b.UserID == "" || b.Server == "" || b.ServerID == uuid.Nil || b.UserID != userID || impersonated || !now.Before(b.ExpiresAt) {
		return oops.E(oops.CodeForbidden, nil, "Only the user who requested this connection can complete it before it expires. Request a new connection link to try again.")
	}
	return nil
}

func validateCodeReconnectBinding(state AuthnChallengeState) error {
	b := state.CodeReconnect
	if b == nil {
		return nil
	}
	if b.Ticket || b.UserID == "" || b.Server == "" || b.ServerID == uuid.Nil || !time.Now().Before(b.ExpiresAt) || state.FocusServer != b.Server || !state.FirstParty {
		return errors.New("invalid or expired code connection request")
	}
	if state.Subject != nil {
		if state.Subject.Kind != urn.SessionSubjectKindUser || state.Subject.ID != b.UserID {
			return errors.New("code connection user mismatch")
		}
		if err := b.authorize(state.AuthorizerUserID, state.AuthorizerImpersonated == nil || *state.AuthorizerImpersonated, time.Now()); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) codeReconnectTicket(r *http.Request, endpoint *ResolvedMcpEndpoint, claim bool) (*CodeReconnectBinding, error) {
	id := r.URL.Query().Get("code_reconnect")
	if id == "" {
		return nil, nil
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid connection request")
	}
	// A copied ticket cannot attach credentials: the IDP callback requires the
	// authoritative user that requested it, before any credential handoff.
	// Peek during preparation; claim atomically only once login setup succeeds.
	var state AuthnChallengeState
	var err error
	if claim {
		state, err = s.authnChallengeCache.GetAndDelete(r.Context(), "authnChallenge:"+id)
	} else {
		state, err = s.authnChallengeCache.Get(r.Context(), "authnChallenge:"+id)
	}
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "connection request expired; request a new link")
	}
	b := state.CodeReconnect
	if b == nil || !b.Ticket || b.UserID == "" || b.Server == "" || b.ServerID == uuid.Nil || !time.Now().Before(b.ExpiresAt) {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid or expired connection request")
	}
	if err := endpoint.ValidateChallenge(r.Context(), state.Endpoint, state.UserSessionIssuerID); err != nil {
		return nil, oauthAuthorityError(err)
	}
	b.Ticket = false
	return b, nil
}
