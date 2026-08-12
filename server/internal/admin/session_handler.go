package admin

import (
	"encoding/json"
	"fmt"
	"net/http"

	"goa.design/goa/v3/security"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// sessionInfo is the response body of GET /admin/session.get.
//
// This endpoint is deliberately not a Goa method. Goa emits every named design
// type into components.schemas of the single OpenAPI document it generates for
// all services, whether or not a path references it. That document is embedded
// and served at /openapi.yaml, and it is also the input to all three Speakeasy
// sources. A Goa method here would therefore push an internal admin type into
// the public document and into the committed SDK bundle, for a shape only the
// hand-written admin dashboard fetch ever reads.
type sessionInfo struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// handleGetSession returns the identity of the admin operator that owns the
// current session. It calls the same Verifier as the Goa admin endpoints, so
// session validation, expiry, and OIDC token refresh behave identically.
func (s *Service) handleGetSession(w http.ResponseWriter, r *http.Request) error {
	// The empty key makes Verifier.Authorize read the session cookie that
	// SessionMiddleware placed in the context. Goa does not bind APIKey schemes
	// to cookies, so the generated endpoints reach the same fallback.
	scheme := security.APIKeyScheme{
		Name:           constants.AdminAuthSecurityScheme,
		Scopes:         []string{},
		RequiredScopes: []string{},
	}

	ctx, err := s.verifier.Authorize(r.Context(), "", &scheme)
	if err != nil {
		return fmt.Errorf("admin auth: %w", err)
	}

	authCtx, ok := contextvalues.GetAdminAuthContext(ctx)
	if !ok {
		return oops.C(oops.CodeUnauthorized)
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(sessionInfo{
		Email: authCtx.Email,
		Name:  authCtx.Name,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode admin session").LogError(ctx, s.logger)
	}

	return nil
}
