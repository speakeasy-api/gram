package usersessions

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// attachRetiredProxy mounts the tombstone for the retired OAuth proxy token
// endpoint. It lives here, alongside the user-session issuer endpoints, because
// that is where the clients it turns away go next: the tombstone exists to move
// them onto the user_session_issuer their MCP server migrated to.
//
// The authorize and register endpoints are simply gone (they 404), which is the
// right answer — a client turned away there re-discovers and completes on the
// issuer-gated path. The token endpoint needs a live handler so a stale refresh
// exchange gets invalid_grant rather than a 404; see handleRetiredProxyToken.
func attachRetiredProxy(mux goahttp.Muxer, service *Service) {
	o11y.AttachHandler(mux, "POST", "/oauth/{mcpSlug}/token", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.handleRetiredProxyToken).ServeHTTP(w, r)
	})
}

// handleRetiredProxyToken answers the retired OAuth proxy token endpoint. The
// proxy serving path is gone, so any client still exchanging a token here holds
// a proxy refresh token minted before its MCP server migrated to a
// user_session_issuer — where, previously, it would have kept exchanging it
// indefinitely, outside the issuer's consent, session duration and revocation.
//
// Answer RFC 6749 §5.2 invalid_grant — the signal a client acts on by
// discarding the token and re-running authorization — rather than a 404, which
// reads as "server gone" and would strand the client on a dead refresh token.
// Discovery already advertises the issuer's endpoints, so the re-authorization
// completes on the issuer-gated path.
func (s *Service) handleRetiredProxyToken(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	s.logger.InfoContext(ctx, "refused retired proxy token exchange",
		attr.SlogToolsetMCPSlug(chi.URLParam(r, "mcpSlug")))

	w.Header().Set("Content-Type", "application/json")
	// Match the other OAuth token handlers: an intermediary must not replay a
	// stale invalid_grant to a later client.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusBadRequest)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"error":             "invalid_grant",
		"error_description": "This MCP server has moved to a new authorization server. Re-authorize to continue.",
	}); err != nil {
		s.logger.ErrorContext(ctx, "failed to encode invalid_grant response", attr.SlogError(err))
	}
	return nil
}
