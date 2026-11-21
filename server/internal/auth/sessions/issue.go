package sessions

import (
	"context"
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/server/internal/constants"
)

// Issue stores a new dashboard session for a subject the caller has already
// authenticated, then writes its cookie. The cookie is written only once the
// session is stored. issuedAt is when authentication started, for the
// sign-out-everywhere epoch (see StoreSessionIssuedAt).
func (s *Manager) Issue(ctx context.Context, w http.ResponseWriter, session Session, issuedAt time.Time) error {
	if err := s.StoreSessionIssuedAt(ctx, session, issuedAt); err != nil {
		return err
	}
	//nolint:exhaustruct_v5 // only desired fields — avoid unexpected zero-value behavior
	http.SetCookie(w, &http.Cookie{
		Name:     constants.SessionCookie,
		Value:    session.SessionID,
		MaxAge:   constants.SessionCookieMaxAgeSeconds,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// ConfineUser pins every session of userID to organizationID in this process:
// UpdateSession then refuses to move one to any other organization, which is
// how auth.switchScopes and auth.enterDemo change a session's organization.
// The end-to-end test control surface confines its test user on a pull request
// preview, so a session it issues never names an organization that exists
// outside that preview. Call it before the server starts serving.
func (s *Manager) ConfineUser(userID, organizationID string) {
	if s.confined == nil {
		s.confined = map[string]string{}
	}
	s.confined[userID] = organizationID
}
