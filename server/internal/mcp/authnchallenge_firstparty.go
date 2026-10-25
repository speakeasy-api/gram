package mcp

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// HandleFirstPartyConnect is the chi handler at
// `GET /mcp/{mcpSlug}/connect/first-party`. It
// resolves the slug to a `/mcp`-keyed ResolvedMcpEndpoint and delegates to
// ServeFirstPartyConnect — the dashboard's entry point for linking an
// issuer-gated server's upstream sessions.
func (s *Service) HandleFirstPartyConnect(w http.ResponseWriter, r *http.Request) error {
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
	return s.ServeFirstPartyConnect(w, r, endpoint)
}

// ServeFirstPartyConnect is the dashboard's entry point for establishing the
// upstream remote_sessions an issuer-gated MCP server needs. It mints a
// first-party authn challenge and bounces through the gram server's own IDP
// login — the same flow a real MCP client runs via /mcp/{slug}/authorize —
// rather than borrowing the dashboard's gram_session.
//
// This is deliberately decoupled from the dashboard session: the subject is
// stamped onto the challenge by HandleIDPCallback from authoritative IDP
// claims, and the only state is the challenge in Redis keyed by the OIDC
// `state` param. Federation uses a separate short-lived browser-binding cookie,
// never the dashboard session (and the org-membership gate in
// HandleIDPCallback enforces access once the IDP identifies the user).
//
// No ClientID/RedirectURI: a first-party challenge has no MCP client to grant
// to or redirect back to. Once the user links every card on the consent page,
// the flow is terminal and the page closes its dashboard-opened tab.
//
// On a gateway, a `server` query parameter names the server to connect, and
// the flow lands on that server's connect page. A gateway tool call links here
// when a server cannot be reached without the user's sign-in.
func (s *Service) ServeFirstPartyConnect(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) error {
	if s.redirectToAuthorizationServer(w, r, endpoint) {
		return nil
	}
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)

	focusServer := r.URL.Query().Get("server")
	if len(focusServer) > maxFocusServerLength {
		return oops.E(oops.CodeBadRequest, nil, "server is too long").LogError(ctx, logger)
	}

	csrfToken, err := generateOpaqueToken()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "generate consent csrf token").LogError(ctx, logger)
	}

	baseURL := s.BaseURLForRequest(r)
	flowID := uuid.NewString()
	challengeID := uuid.NewString()
	endpointRef, err := endpoint.EndpointRef(ctx, s.db, baseURL)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "capture OAuth endpoint authority").LogWarn(ctx, logger)
	}
	challengeState := AuthnChallengeState{ConsentBrowserHash: "", FederatedBinding: nil, DelegationRetryUsed: false,
		Browser:    nil,
		Federation: nil,

		ID:                       challengeID,
		FlowID:                   flowID,
		UserSessionIssuerID:      endpoint.UserSessionIssuerID,
		AuthorizerUserID:         "",
		AuthorizerImpersonated:   nil,
		SignInID:                 nil,
		FreshLoginForced:         false,
		AgentAuthorizationTarget: nil,
		Endpoint:                 endpointRef,
		ClientID:                 "",
		RegisteredClientID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RedirectURI:              "",
		State:                    "",
		CodeChallenge:            "",
		CodeChallengeMethod:      "",
		CSRFToken:                csrfToken,

		// Subject is stamped by HandleIDPCallback from authoritative IDP claims.
		Subject:    nil,
		CreatedAt:  time.Now(),
		FirstParty: true,
		// Auto-connect has not run for a challenge this new.
		AutoConnectDone: false,
		FocusServer:     focusServer,
	}
	if err := s.authnChallengeCache.Store(ctx, challengeState); err != nil {
		return oops.E(oops.CodeUnexpected, err, "store authn challenge state").LogError(ctx, logger)
	}

	s.metrics.RecordOAuthFlowStarted(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug)

	federatedURL, err := s.prepareFederatedLogin(w, r, endpoint, &challengeState)
	if err != nil {
		_, _ = s.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+challengeState.ID)
		failureCode, cause := federatedFailure(err)
		return s.finishFederatedFailure(w, r, endpoint, challengeState, mcpmetrics.OAuthFlowStageAuthorize, failureCode, cause, "Federated login configuration is unavailable. Restart login or contact your administrator", false)
	}
	if federatedURL != nil {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.Redirect(w, r, federatedURL.String(), http.StatusFound)
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
		s.metrics.RecordOAuthFlowFailed(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug, mcpmetrics.OAuthFlowStageAuthorize)
		return oops.E(oops.CodeUnexpected, err, "build IDP authorization URL").LogError(ctx, logger)
	}

	logger.InfoContext(ctx, "started first-party connect flow", attr.SlogOAuthFlowID(flowID))
	http.Redirect(w, r, idpURL.String(), http.StatusFound)
	return nil
}
