// OAuth 2.1 authorization endpoint (RFC 6749 §4.1.1) on the issuer-gated
// authn-challenge surface. Mints an AuthnChallengeState in Redis and 302s
// the user to either the Speakeasy IDP (private toolsets) or the consent
// UI (public toolsets).

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
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
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").LogError(ctx, s.logger)
	}
	logger := s.logger.With(attr.SlogToolsetMCPSlug(mcpSlug))
	endpoint, err := s.LoadResolvedMcpEndpointBySlug(ctx, logger, mcpSlug, "mcp")
	if err != nil {
		return err
	}
	return s.ServeAuthorize(w, r, endpoint)
}

// ServeAuthorize is the post-resolution entry point for the OAuth 2.1
// authorize endpoint, shared by /mcp's HandleAuthorize (toolset-keyed)
// and /x/mcp's mcp_endpoint-keyed route registration.
func (s *Service) ServeAuthorize(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) error {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)

	req := usersessions.AuthorizationRequestFromQuery(r.URL.Query())
	req.SetDefaults()

	// Stamp the presented client_id before anything can reject the request.
	// Without this it only reaches the logs on the post-resolution "oauth
	// flow started" line, so every REJECTED attempt — an unknown client, a
	// malformed CIMD document, an admission denial — is invisible by URI.
	// That URI is the single most useful field when diagnosing why a real
	// client cannot connect, since the client itself surfaces only the
	// error_description to its user.
	logger = logger.With(attr.SlogOAuthClientID(truncateClientIDForLog(req.ClientID)))

	// RFC 6749 §4.1.2.1 wants validation errors carried back to the client
	// via redirect when we can trust the redirect_uri, and surfaced inline
	// otherwise. That motivates the two-phase split: validate the fields
	// we need to trust the URI first, then validate the rest once we have.
	if err := req.ValidateRedirectableFields(); err != nil {
		return writeAuthorizeOAuthError(ctx, w, logger, http.StatusBadRequest, err)
	}

	// URL-shaped client_ids resolve via CIMD here (admission-gated, inside
	// the resolver). All resolution failures render inline per RFC 6749
	// §4.1.2.1 — the redirect_uri of an unresolved client cannot be
	// trusted — and a document fetch failure aborts the request per
	// draft-ietf-oauth-client-id-metadata-document-02 §5.1 (fail closed,
	// no stale fallback).
	client, err := s.resolveUserSessionClient(ctx, logger, endpoint, req.ClientID, resolveClientCIMD)
	if err != nil {
		if admissionErr, ok := errors.AsType[*admission.DenialError](err); ok {
			// Checked before the generic *oauthwire.Error branch below, since an
			// admission denial is policy rather than a spec violation and
			// carries its own actionable description. Already logged with
			// the presented client_id inside admitCIMDClient.
			return writeAuthorizeError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", admissionErr.Description())
		}
		if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
			return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, oauthErr.Code, oauthErr.Description)
		}
		if errors.Is(err, errCIMDFetchFailed) {
			// The cause may name internal network conditions (SSRF denials,
			// DNS failures); log it and keep the wire response generic. A
			// fetch failure is transient from the client's perspective — the
			// document host may just be briefly unreachable — so signal
			// retry-later (temporarily_unavailable, RFC 6749 §4.1.2.1)
			// rather than a permanent invalid_client that would make SDKs
			// stop retrying.
			logger.InfoContext(ctx, "cimd document fetch failed", attr.SlogError(err))
			return writeAuthorizeError(ctx, w, logger, http.StatusServiceUnavailable, "temporarily_unavailable", "failed to fetch client metadata document")
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return writeAuthorizeError(ctx, w, logger, http.StatusUnauthorized, "invalid_client", "unknown client_id")
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").LogError(ctx, logger)
	}
	if !redirectURIMatches(client, req.RedirectURI) {
		return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, "invalid_request", "redirect_uri is not registered for this client")
	}

	// The origin this request was addressed at. It is the mint origin by
	// definition — the challenge below snapshots it — and it is what the AS
	// metadata document advertises as the issuer, so both the error redirect
	// below and every response built later in the flow agree on it.
	baseURL := s.BaseURLForRequest(r)

	// The endpoint's canonical URI at the address this request arrived on. One
	// value serves three contracts: the RFC 9207 `iss` on every authorization
	// response, the AS metadata issuer, and the RFC 9728 protected-resource
	// `resource`. That identity is what lets the RFC 8707 check below compare
	// against a value the client was already handed.
	issuer, err := endpoint.RootURL(baseURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build authorization response issuer").LogError(ctx, logger)
	}

	// At this point the redirect_uri is trusted (matched against the
	// registered set on the client row), so RFC 6749 §4.1.2.1 requires that
	// any remaining validation errors are forwarded to the client by 302
	// rather than rendered inline — otherwise the MCP client has no way to
	// observe the failure. The two-phase Validate split exists to make this
	// switch unambiguous.
	if err := req.ValidatePostRedirect(); err != nil {
		return redirectAuthorizeOAuthError(ctx, w, r, logger, issuer, req.RedirectURI, req.State, "", err)
	}
	// RFC 8707 §2: a resource naming some other server means the client
	// believes it is getting a token for an endpoint this one will never mint
	// for. Rejecting makes that misconfiguration visible at the point it
	// happens instead of at first use.
	if err := oauthwire.ValidateResourceIndicator(req.Resource, issuer); err != nil {
		return redirectAuthorizeOAuthError(ctx, w, r, logger, issuer, req.RedirectURI, req.State, "resource_mismatch", err)
	}

	challengeID := uuid.NewString()
	// flowID is the stable correlation key for this whole OAuth flow. It is
	// minted once here and preserved across the idp_callback cache-key
	// rotation and the consent→/token handoff, unlike challengeID which is
	// the (rotating) Redis cache key. From here on the request logger carries
	// it so every line in the flow shares one filterable value.
	flowID := uuid.NewString()
	logger = logger.With(attr.SlogOAuthFlowID(flowID))

	csrfToken, err := generateOpaqueToken()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "generate consent csrf token").LogError(ctx, logger)
	}
	// Ambient cookies / Bearer tokens MUST NOT identify the caller on
	// this endpoint — /authorize is reachable cross-site, so honouring
	// them turns it into a CSRF primitive against the resulting
	// remote_sessions row. Public callers that want a user-bound
	// session opt in via requireUserIdentity; HandleIDPCallback then
	// stamps Subject from authoritative IDP claims.
	forceIDP := !endpoint.IsPublic || req.RequireUserIdentity

	var subject *urn.SessionSubject
	if !forceIDP {
		sub := urn.NewAnonymousSubject(uuid.NewString())
		subject = &sub
	}

	challengeState := AuthnChallengeState{
		ID:                  challengeID,
		FlowID:              flowID,
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		Endpoint:            endpoint.EndpointRef(baseURL),
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		State:               req.State,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		CSRFToken:           csrfToken,
		Subject:             subject,
		CreatedAt:           time.Now(),
		FirstParty:          false,
	}

	if err := s.authnChallengeCache.Store(ctx, challengeState); err != nil {
		return oops.E(oops.CodeUnexpected, err, "store authn challenge state").LogError(ctx, logger)
	}

	// Flow start: counted exactly once per minted challenge, the unit the
	// companion completion-ratio monitor divides terminal outcomes against.
	s.metrics.RecordOAuthFlowStarted(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug)
	logger.InfoContext(ctx, "oauth flow started")

	if forceIDP {
		callbackURL, err := endpoint.IDPCallbackURL(s.serverURL.String())
		if err != nil {
			s.metrics.RecordOAuthFlowFailed(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug, mcpmetrics.OAuthFlowStageAuthorize)
			return oops.E(oops.CodeUnexpected, err, "build IDP callback URL").LogError(ctx, logger)
		}
		idpURL, err := s.identityResolver.BuildAuthorizationURL(ctx, identity.AuthorizationURLParams{
			CallbackURL:     callbackURL,
			Scope:           "",
			State:           challengeID,
			ScopesSupported: nil,
			LoginHint:       "",
			ScreenHint:      "",
		})
		if err != nil {
			// A failure to build the IDP authorization URL typically means the
			// issuer's IDP wiring is misconfigured — a config-class flow failure.
			s.metrics.RecordOAuthFlowFailed(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug, mcpmetrics.OAuthFlowStageAuthorize)
			return oops.E(oops.CodeUnexpected, err, "build IDP authorization URL").LogError(ctx, logger)
		}
		http.Redirect(w, r, idpURL.String(), http.StatusFound)
		return nil
	}

	consentURL, err := endpoint.ConsentURL(baseURL, challengeID)
	if err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug, mcpmetrics.OAuthFlowStageAuthorize)
		return oops.E(oops.CodeUnexpected, err, "build consent URL").LogError(ctx, logger)
	}
	http.Redirect(w, r, consentURL, http.StatusFound)
	return nil
}

// writeAuthorizeOAuthError unwraps a *oauthwire.Error to its code +
// description and forwards to writeAuthorizeError. Falls back to a generic
// invalid_request if err is something else (shouldn't happen — Validate
// returns *oauthwire.Error).
func writeAuthorizeOAuthError(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, status int, err error) error {
	var oauthErr *oauthwire.Error
	if errors.As(err, &oauthErr) {
		return writeAuthorizeError(ctx, w, logger, status, oauthErr.Code, oauthErr.Description)
	}
	return writeAuthorizeError(ctx, w, logger, status, "invalid_request", err.Error())
}

// redirectAuthorizeOAuthError redirects the user agent back to the (already
// trusted) redirect_uri with `iss` / `error` / `error_description` / `state`
// query parameters per RFC 6749 §4.1.2.1 and RFC 9207 §2. Callers must only
// invoke this AFTER the supplied redirect_uri has been validated against the
// registered set on the OAuth client row — passing through an untrusted URI
// here would turn the AS into an open redirector.
// failureReason labels the rejection with the same vocabulary the token
// endpoint logs (for example "resource_mismatch"), so one query finds a given
// class of rejection on both legs. Empty when the error code alone identifies
// the cause.
func redirectAuthorizeOAuthError(ctx context.Context, w http.ResponseWriter, r *http.Request, logger *slog.Logger, issuer, redirectURI, originalState, failureReason string, err error) error {
	code := "invalid_request"
	description := err.Error()
	var oauthErr *oauthwire.Error
	if errors.As(err, &oauthErr) {
		code = oauthErr.Code
		description = oauthErr.Description
	}
	args := []any{
		attr.SlogOAuthError(code),
		attr.SlogOAuthErrorDescription(description),
	}
	if failureReason != "" {
		args = append(args, attr.SlogOAuthFailureReason(failureReason))
	}
	logger.InfoContext(ctx, "authorize request rejected (post-redirect)", args...)
	redirect, err := buildClientRedirect(clientRedirectParams{
		RedirectURI:      redirectURI,
		Issuer:           issuer,
		Code:             "",
		State:            originalState,
		ErrorCode:        code,
		ErrorDescription: description,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build client redirect").LogError(ctx, logger)
	}
	http.Redirect(w, r, redirect, http.StatusFound)
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
		return oops.E(oops.CodeUnexpected, err, "failed to marshal authorize error").LogError(ctx, logger)
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
		return oops.E(oops.CodeUnexpected, werr, "failed to write authorize error body").LogError(ctx, logger)
	}
	return nil
}
