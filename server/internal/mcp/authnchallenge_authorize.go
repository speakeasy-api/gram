// OAuth 2.1 authorization endpoint (RFC 6749 §4.1.1) on the issuer-gated
// authn-challenge surface. Mints an AuthnChallengeState in Redis and 302s
// the user to either the Speakeasy IDP (private toolsets) or the consent
// UI (public toolsets).

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// HandleAuthorize implements the OAuth 2.1 authorization endpoint (RFC 6749
// §4.1.1) on the issuer-gated authn-challenge surface. Mounted at
// `GET /mcp/{mcpSlug}/authorize`.
//
// Flow:
//   - validate the request (response_type=code, S256 PKCE, known client,
//     allowed redirect_uri)
//   - mint an AuthnChallengeState in Redis carrying the request context
//   - branch on the toolset's privacy:
//   - private (`!McpIsPublic`): 302 to the Speakeasy IDP login page; on
//     return HandleIDPCallback stamps `user:<id>` onto the state
//   - public (`McpIsPublic`): 302 directly to /connect; HandleConsent's
//     POST stamps `anonymous:<prospective_mcp_session_id>`
func (s *Service) HandleAuthorize(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").Log(ctx, s.logger)
	}

	toolset, customDomainCtx, err := s.loadToolsetFromMcpSlug(ctx, mcpSlug)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, s.logger)
	}

	if !toolset.UserSessionIssuerID.Valid {
		return oops.E(oops.CodeNotFound, nil, "not found")
	}
	if err := s.requireUserSessionIssuer(ctx, toolset); err != nil {
		return err
	}

	logger := s.logger.With(
		attr.SlogToolsetID(toolset.ID.String()),
		attr.SlogProjectID(toolset.ProjectID.String()),
	)

	req := usersessions.AuthorizationRequestFromQuery(r.URL.Query())
	req.SetDefaults()

	// RFC 6749 §4.1.2.1 wants validation errors carried back to the client
	// via redirect when we can trust the redirect_uri, and surfaced inline
	// otherwise. That motivates the two-phase split: validate the fields
	// we need to trust the URI first, then validate the rest once we have.
	if err := req.ValidateRedirectableFields(); err != nil {
		return writeAuthorizeOAuthError(ctx, w, logger, http.StatusBadRequest, err)
	}

	client, err := usersessions_repo.New(s.db).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ClientID:            req.ClientID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return writeAuthorizeError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", "unknown client_id")
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").Log(ctx, logger)
	}
	if !slices.Contains(client.RedirectUris, req.RedirectURI) {
		return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, "invalid_request", "redirect_uri is not registered for this client")
	}

	if err := req.ValidatePostRedirect(); err != nil {
		return writeAuthorizeOAuthError(ctx, w, logger, http.StatusBadRequest, err)
	}

	challengeID := uuid.NewString()

	challengeState := AuthnChallengeState{
		ID:                  challengeID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		ToolsetID:           toolset.ID,
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		State:               req.State,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		// Subject is left nil — HandleIDPCallback (private path) and
		// HandleConsent (public path) stamp it later in the flow.
		Subject:   nil,
		CreatedAt: time.Now(),
	}

	if err := s.authnChallengeCache.Store(ctx, challengeState); err != nil {
		return oops.E(oops.CodeUnexpected, err, "store authn challenge state").Log(ctx, logger)
	}

	baseURL := s.serverURL.String()
	if customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain)
	}

	if !toolset.McpIsPublic {
		callbackURL := fmt.Sprintf("%s/mcp/%s/idp_callback", baseURL, mcpSlug)
		idpURL, err := s.sessions.BuildAuthorizationURL(ctx, sessions.AuthURLParams{
			CallbackURL:     callbackURL,
			Scope:           "",
			State:           challengeID,
			ClientID:        "",
			ScopesSupported: nil,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "build IDP authorization URL").Log(ctx, logger)
		}
		http.Redirect(w, r, idpURL.String(), http.StatusFound)
		return nil
	}

	// Public toolset: skip IDP, route straight to consent. The anonymous sub
	// is minted on the consent POST (per plan).
	consentURL := fmt.Sprintf("%s/mcp/%s/connect?state=%s", baseURL, mcpSlug, url.QueryEscape(challengeID))
	http.Redirect(w, r, consentURL, http.StatusFound)
	return nil
}

// writeAuthorizeOAuthError unwraps a *usersessions.OAuthError to its code +
// description and forwards to writeAuthorizeError. Falls back to a generic
// invalid_request if err is something else (shouldn't happen — Validate
// returns *OAuthError).
func writeAuthorizeOAuthError(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, status int, err error) error {
	var oauthErr *usersessions.OAuthError
	if errors.As(err, &oauthErr) {
		return writeAuthorizeError(ctx, w, logger, status, oauthErr.Code, oauthErr.Description)
	}
	return writeAuthorizeError(ctx, w, logger, status, "invalid_request", err.Error())
}

// writeAuthorizeError emits an OAuth 2.1 authorization error (RFC 6749
// §4.1.2.1) inline as a JSON body. We don't redirect to redirect_uri because
// the request hasn't been validated to that point — per RFC 6749 §3.1.2.4, an
// invalid redirect_uri must NOT be redirected to.
func writeAuthorizeError(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, status int, code, description string) error {
	body, err := json.Marshal(map[string]string{
		"error":             code,
		"error_description": description,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to marshal authorize error").Log(ctx, logger)
	}

	logger.InfoContext(ctx, "authorize request rejected",
		attr.SlogOAuthError(code),
		attr.SlogOAuthErrorDescription(description),
	)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	if _, werr := w.Write(body); werr != nil {
		return oops.E(oops.CodeUnexpected, werr, "failed to write authorize error body").Log(ctx, logger)
	}
	return nil
}
