package auth

import (
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (s *Service) handleStartSupportSession(w http.ResponseWriter, r *http.Request) error {
	ctx, err := s.sessions.AuthenticateWithCookie(r.Context())
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authentication required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid support session request")
	}

	location, err := s.StartSupportSession(ctx, r.PostForm.Get("organization_slug"))
	if err != nil {
		return err
	}

	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r.WithContext(ctx), location, http.StatusSeeOther)
	return nil
}
