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
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
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
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").LogError(ctx, s.logger)
	}
	logger := s.logger.With(attr.SlogToolsetMCPSlug(mcpSlug))
	endpoint, err := s.LoadResolvedMcpEndpointBySlug(ctx, logger, mcpSlug, mcproute.MCP)
	if err != nil {
		return err
	}
	return s.ServeAuthorize(w, r, endpoint)
}

// ServeAuthorize is the post-resolution entry point for the OAuth 2.1
// authorize endpoint, called by HandleAuthorize after slug resolution.
func (s *Service) ServeAuthorize(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) (err error) {
	// A bounce to the shared authorization server is served again there, and
	// observed once there.
	if s.redirectToAuthorizationServer(w, r, endpoint) {
		return nil
	}
	r, obs := observeInbound(r, endpoint, attemptStageInboundAuthorize)
	defer func() { s.finishInbound(r.Context(), obs, err) }()
	return s.serveAuthorize(w, r, endpoint)
}

func (s *Service) serveAuthorize(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) error {
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
	// needed to match the URI first, then validate the rest.
	if err := req.ValidateRedirectableFields(); err != nil {
		return writeAuthorizeOAuthError(ctx, w, logger, http.StatusBadRequest, err)
	}

	// The origin of the endpoint's resource. It is the mint origin by
	// definition — the challenge below snapshots it — and the issuer derives
	// from it, so both the error redirect below and every response built later
	// in the flow agree on it. On the authentication host it is the platform
	// origin, and on a shared authorization server it is the origin of the
	// resource the client named.
	baseURL := s.resourceBaseURL(r, endpoint)

	// A client_id naming this endpoint is a misconfigured client, not a client
	// metadata document. Refused ahead of admission so every admission mode
	// gives the same actionable answer, and ahead of the fetch it would
	// otherwise send back to this server.
	namesEndpoint, err := s.clientIDNamesEndpoint(endpoint, baseURL, req.ClientID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check client_id against endpoint URLs").LogError(ctx, logger)
	}
	if namesEndpoint {
		logger.InfoContext(ctx, "oauth authorize rejected client_id naming this endpoint", attr.SlogOAuthFailureReason(clientIDNamesEndpointReason))
		inboundObservationFrom(ctx).refuse(classConfiguration, clientIDNamesEndpointReason)
		return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidClient, clientIDNamesEndpointDescription)
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
			inboundObservationFrom(ctx).refuse(classConfiguration, cimdDenialReason(admissionErr.Reason))
			return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidClient, admissionErr.Description())
		}
		if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
			code := oauthErr.Code
			if code == oautherr.CodeInvalidClientMetadata || code == oautherr.CodeInvalidRedirectURI {
				code = oautherr.CodeInvalidClient
			}
			return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, code, oauthErr.Description)
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
			inboundObservationFrom(ctx).refuse(classEndpoint, reasonCIMDFetchFailed)
			return writeAuthorizeError(ctx, w, logger, http.StatusServiceUnavailable, "temporarily_unavailable", "failed to fetch client metadata document")
		}
		if errors.Is(err, pgx.ErrNoRows) {
			inboundObservationFrom(ctx).refuse(classConfiguration, reasonUnknownClientID)
			return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidClient, "unknown client_id")
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").LogError(ctx, logger)
	}
	if !redirectURIMatches(client, req.RedirectURI) {
		inboundObservationFrom(ctx).refuse(classConfiguration, reasonRedirectURINotRegistered)
		return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, "invalid_request", "redirect_uri is not registered for this client")
	}
	inboundObservationFrom(ctx).setClient(client.ID)

	// Shadow AI blocking, refused inline for the same reason an admission
	// denial is: it is policy about the client itself, so forwarding it to
	// the client's redirect_uri would hand a blocked tool a normal-looking
	// error page instead of telling the person what happened. It runs after
	// resolution because only a resolved client_id is a credential the server
	// verified.
	if err := s.checkAIToolGatewayBlock(ctx, logger, endpoint.OrganizationID, req.ClientID); err != nil {
		if blockedErr, ok := errors.AsType[*AIToolBlockedError](err); ok {
			inboundObservationFrom(ctx).refuse(classConfiguration, reasonClientBlocked)
			return writeAuthorizeError(ctx, w, logger, http.StatusBadRequest, oautherr.CodeInvalidClient, blockedErr.Description())
		}
		if errors.Is(err, ErrAIToolBlockCheckUnavailable) {
			return writeAuthorizeError(ctx, w, logger, http.StatusServiceUnavailable, "temporarily_unavailable", "cannot determine whether this client is permitted right now")
		}
		return oops.E(oops.CodeUnexpected, err, "check ai tool gateway block").LogError(ctx, logger)
	}

	// The RFC 9207 `iss` on every authorization response, equal to the AS
	// metadata issuer. It names the authentication host when the endpoint's
	// issuer opts in, and the endpoint's canonical URI otherwise.
	issuer, err := s.issuerURL(endpoint, baseURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build authorization response issuer").LogError(ctx, logger)
	}
	// The RFC 9728 protected-resource `resource`, which always stays on the
	// MCP host. It is what the RFC 8707 check below compares against: a value
	// the client was already handed.
	resource, err := endpoint.RootURL(baseURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build resource identifier").LogError(ctx, logger)
	}

	// Registered callbacks can still be attacker-controlled. Before authentication,
	// only loopback destinations receive automatic error redirects.
	if err := req.ValidatePostRedirect(); err != nil {
		return writePreAuthenticationAuthorizeError(ctx, w, r, logger, issuer, req.RedirectURI, req.State, "", authorizationErrorPage(client, req.ClientID, endpoint.Slug), err)
	}
	// RFC 8707 §2: a resource naming some other server means the client
	// believes it is getting a token for an endpoint this one will never mint
	// for. Rejecting makes that misconfiguration visible at the point it
	// happens instead of at first use.
	if err := oauthwire.ValidateResourceIndicators(req.Resources, resource); err != nil {
		return writePreAuthenticationAuthorizeError(ctx, w, r, logger, issuer, req.RedirectURI, req.State, "resource_mismatch", authorizationErrorPage(client, req.ClientID, endpoint.Slug), err)
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

	endpointRef, err := endpoint.EndpointRef(ctx, s.db, baseURL)
	if err != nil {
		if errors.Is(err, networkingress.ErrAuthorityUnavailable) {
			s.metrics.RecordOAuthAuthorityUnavailable(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug, mcpmetrics.OAuthFlowStageAuthorize)
			return oauthAuthorityError(err).LogError(ctx, logger)
		}
		return oops.E(oops.CodeBadRequest, err, "capture OAuth endpoint authority").LogWarn(ctx, logger)
	}
	agentTarget, _ := agentAuthorizationTarget(endpoint)
	challengeState := AuthnChallengeState{ConsentBrowserHash: "", FederatedBinding: nil, DelegationRetryUsed: false,
		Browser:                  nil,
		Federation:               nil,
		ID:                       challengeID,
		FlowID:                   flowID,
		UserSessionIssuerID:      endpoint.UserSessionIssuerID,
		AuthorizerUserID:         "",
		AuthorizerImpersonated:   nil,
		SignInID:                 nil,
		FreshLoginForced:         false,
		AgentAuthorizationTarget: agentTarget,
		Endpoint:                 endpointRef,
		ClientID:                 req.ClientID,
		RedirectURI:              req.RedirectURI,
		State:                    req.State,
		CodeChallenge:            req.CodeChallenge,
		CodeChallengeMethod:      req.CodeChallengeMethod,
		CSRFToken:                csrfToken,
		Subject:                  subject,
		CreatedAt:                time.Now(),
		FirstParty:               false,

		// Auto-connect has not run for a challenge this new.
		AutoConnectDone: false,
		FocusServer:     "",
	}

	if endpoint.servingSharedAuthorizationServer() != nil {
		if err := bindConsentBrowser(w, &challengeState); err != nil {
			return oops.E(oops.CodeUnexpected, err, "bind consent browser").LogError(ctx, logger)
		}
	}

	if err := s.authnChallengeCache.Store(ctx, challengeState); err != nil {
		return oops.E(oops.CodeUnexpected, err, "store authn challenge state").LogError(ctx, logger)
	}

	// Flow start: counted exactly once per minted challenge, the unit the
	// companion completion-ratio monitor divides terminal outcomes against.
	s.metrics.RecordOAuthFlowStarted(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug)
	logger.InfoContext(ctx, "oauth flow started")

	if forceIDP {
		federatedURL, err := s.prepareFederatedLogin(w, r, endpoint, &challengeState)
		if err != nil {
			_, _ = s.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+challengeState.ID)
			failureCode, cause := federatedFailure(err)
			inboundObservationFrom(ctx).refuse(classInternal, reasonFederationUnavailable)
			return s.finishFederatedFailure(w, r, endpoint, challengeState, mcpmetrics.OAuthFlowStageAuthorize, failureCode, cause, "Federated login configuration is unavailable. Restart login or contact your administrator", false)
		}
		if federatedURL != nil {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
			http.Redirect(w, r, federatedURL.String(), http.StatusFound)
			inboundObservationFrom(ctx).accept()
			return nil
		}
		callbackURL, err := endpoint.IDPCallbackURL(s.outboundOrigin().String())
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
			Prompt:          "",
		})
		if err != nil {
			// A failure to build the IDP authorization URL typically means the
			// issuer's IDP wiring is misconfigured — a config-class flow failure.
			s.metrics.RecordOAuthFlowFailed(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug, mcpmetrics.OAuthFlowStageAuthorize)
			return oops.E(oops.CodeUnexpected, err, "build IDP authorization URL").LogError(ctx, logger)
		}
		http.Redirect(w, r, idpURL.String(), http.StatusFound)
		inboundObservationFrom(ctx).accept()
		return nil
	}

	// Consent is an authorization server page, so it is served where the
	// issuer lives.
	consentURL, err := s.challengeConsentURL(endpoint, challengeState)
	if err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug, mcpmetrics.OAuthFlowStageAuthorize)
		return oops.E(oops.CodeUnexpected, err, "build consent URL").LogError(ctx, logger)
	}
	http.Redirect(w, r, consentURL, http.StatusFound)
	inboundObservationFrom(ctx).accept()
	return nil
}

// writeAuthorizeOAuthError unwraps a *oauthwire.Error to its code +
// description and forwards to writeAuthorizeError. Falls back to a generic
// invalid_request if err is something else (shouldn't happen — Validate
// returns *oauthwire.Error).
func writeAuthorizeOAuthError(ctx context.Context, w http.ResponseWriter, logger *slog.Logger, status int, err error) error {
	if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
		return writeAuthorizeError(ctx, w, logger, status, oauthErr.Code, oauthErr.Description)
	}
	return writeAuthorizeError(ctx, w, logger, status, "invalid_request", err.Error())
}

// writePreAuthenticationAuthorizeError sends errors to a registered callback
// automatically only for loopback destinations. Other callbacks require an
// explicit user click, preventing DCR and CIMD phishing redirects (RFC 9700 §4.11.2).
// failureReason preserves resource-rejection telemetry across response modes.
// page names the client and server the way the consent page does; its error
// and callback fields are filled in here.
func writePreAuthenticationAuthorizeError(ctx context.Context, w http.ResponseWriter, r *http.Request, logger *slog.Logger, issuer, redirectURI, originalState, failureReason string, page oauthui.AuthorizationError, err error) error {
	code := "invalid_request"
	description := err.Error()
	if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
		code = oauthErr.Code
		description = oauthErr.Description
	}
	obs := inboundObservationFrom(ctx)
	obs.note(failureReason)
	obs.answer(http.StatusBadRequest, code)
	args := []any{
		attr.SlogOAuthError(code),
		attr.SlogOAuthErrorDescription(description),
	}
	if failureReason != "" {
		args = append(args, attr.SlogOAuthFailureReason(failureReason))
	}
	logger.InfoContext(ctx, "authorize request rejected before authentication", args...)
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
	if !oauthwire.MayAutoRedirectBeforeAuthentication(redirectURI) {
		page.CallbackURL, page.Code, page.Description = redirect, code, description
		if err := oauthui.WriteAuthorizationError(ctx, w, page); err != nil {
			return oops.E(oops.CodeUnexpected, err, "render authorization error page").LogError(ctx, logger)
		}
		return nil
	}
	http.Redirect(w, r, redirect, http.StatusFound)
	return nil
}

// authorizationErrorPage names the client and server on the authorization
// error page as the consent page does. clientID is the value the request
// presented, which the browser already holds.
func authorizationErrorPage(client *usersessions_repo.UserSessionClient, clientID, serverName string) oauthui.AuthorizationError {
	page := oauthui.AuthorizationError{
		CallbackURL:    "",
		Code:           "",
		Description:    "",
		ClientName:     client.ClientName,
		ClientID:       clientID,
		ClientIDOrigin: "",
		ServerName:     serverName,
	}
	if client.ClientIDMetadataUri.Valid {
		if u, err := url.Parse(client.ClientIDMetadataUri.String); err == nil {
			page.ClientIDOrigin = u.Host
		}
	}
	return page
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
	inboundObservationFrom(ctx).answer(status, code)

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
