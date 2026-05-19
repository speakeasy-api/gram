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
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
//   - public (`McpIsPublic`): stamp an anonymous subject, then 302 directly
//     to /connect
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

	// At this point the redirect_uri is trusted (matched against the
	// registered set on the client row), so RFC 6749 §4.1.2.1 requires that
	// any remaining validation errors are forwarded to the client by 302
	// rather than rendered inline — otherwise the MCP client has no way to
	// observe the failure. The two-phase Validate split exists to make this
	// switch unambiguous.
	if err := req.ValidatePostRedirect(); err != nil {
		return redirectAuthorizeOAuthError(ctx, w, r, logger, req.RedirectURI, req.State, err)
	}

	challengeID := uuid.NewString()
	csrfToken, err := generateOpaqueToken()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "generate consent csrf token").Log(ctx, logger)
	}
	customDomainID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if customDomainCtx != nil {
		customDomainID = uuid.NullUUID{UUID: customDomainCtx.DomainID, Valid: true}
	}
	var subject *urn.SessionSubject
	if toolset.McpIsPublic {
		sub := urn.NewAnonymousSubject(uuid.NewString())
		subject = &sub
	}

	challengeState := AuthnChallengeState{
		ID:                  challengeID,
		UserSessionIssuerID: toolset.UserSessionIssuerID.UUID,
		Endpoint: LegacyMcpEndpointRef{
			McpSlug:        mcpSlug,
			CustomDomainID: customDomainID,
		},
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		State:               req.State,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		CSRFToken:           csrfToken,
		Subject:             subject,
		CreatedAt:           time.Now(),
	}

	if err := s.authnChallengeCache.Store(ctx, challengeState); err != nil {
		return oops.E(oops.CodeUnexpected, err, "store authn challenge state").Log(ctx, logger)
	}

	baseURL := s.serverURL.String()
	if customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain)
	}

	if !toolset.McpIsPublic {
		callbackURL, err := url.JoinPath(s.serverURL.String(), "mcp", "idp_callback")
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "build IDP callback URL").Log(ctx, logger)
		}
		idpURL, err := s.identityResolver.BuildAuthorizationURL(ctx, identity.AuthorizationURLParams{
			CallbackURL:     callbackURL,
			Scope:           "",
			State:           challengeID,
			ScopesSupported: nil,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "build IDP authorization URL").Log(ctx, logger)
		}
		http.Redirect(w, r, idpURL.String(), http.StatusFound)
		return nil
	}

	// Public toolset: skip IDP and route straight to consent.
	consentURL, err := buildConsentURL(baseURL, mcpSlug, challengeID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build consent URL").Log(ctx, logger)
	}
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

// redirectAuthorizeOAuthError redirects the user agent back to the (already
// trusted) redirect_uri with `error` / `error_description` / `state` query
// parameters per RFC 6749 §4.1.2.1. Callers must only invoke this AFTER the
// supplied redirect_uri has been validated against the registered set on
// the OAuth client row — passing through an untrusted URI here would turn
// the AS into an open redirector.
func redirectAuthorizeOAuthError(ctx context.Context, w http.ResponseWriter, r *http.Request, logger *slog.Logger, redirectURI, originalState string, err error) error {
	code := "invalid_request"
	description := err.Error()
	var oauthErr *usersessions.OAuthError
	if errors.As(err, &oauthErr) {
		code = oauthErr.Code
		description = oauthErr.Description
	}
	logger.InfoContext(ctx, "authorize request rejected (post-redirect)",
		attr.SlogOAuthError(code),
		attr.SlogOAuthErrorDescription(description),
	)
	http.Redirect(w, r, buildClientRedirect(redirectURI, "", originalState, code, description), http.StatusFound)
	return nil
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
