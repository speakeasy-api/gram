package mcp

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// HandleFirstPartyConnect is the chi handler at
// `GET /mcp/{mcpSlug}/connect/first-party`. It
// resolves the slug to a `/mcp`-keyed ResolvedMcpEndpoint and delegates to
// ServeFirstPartyConnect — the dashboard's entry point for linking an
// issuer-gated server's upstream sessions.
func (s *Service) HandleFirstPartyConnect(w http.ResponseWriter, r *http.Request) (retErr error) {
	defer func() { retErr = s.firstPartyBrowserError(retErr) }()
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
func (s *Service) ServeFirstPartyConnect(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) (retErr error) {
	defer func() { retErr = s.firstPartyBrowserError(retErr) }()
	if s.redirectToAuthorizationServer(w, r, endpoint) {
		return nil
	}
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)
	reconnect, err := s.consumeWorkloadReconnect(r, endpoint)
	if err != nil {
		return err
	}

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
	attemptID := r.URL.Query().Get("binding_refresh_attempt")
	if attemptID != "" {
		if s.bindingRefresh == nil {
			return oops.C(oops.CodeUnavailable)
		}
	}
	reauthenticateClientID := uuid.Nil
	if requested := r.URL.Query().Get("reauthenticate_client_id"); requested != "" {
		parsed, err := uuid.Parse(requested)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "invalid reconnect client")
		}
		reauthenticateClientID = parsed
	}
	endpointRef, err := endpoint.EndpointRef(ctx, s.db, baseURL)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "capture OAuth endpoint authority").LogWarn(ctx, logger)
	}
	challengeState := AuthnChallengeState{PendingDelegation: nil, ConsentBrowserHash: "", FederatedBinding: nil, DelegationRetryUsed: false,
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
		RedirectURI:              "",
		State:                    "",
		CodeChallenge:            "",
		CodeChallengeMethod:      "",
		CSRFToken:                csrfToken,

		// Subject is stamped by HandleIDPCallback from authoritative IDP claims.
		Subject:                 nil,
		CreatedAt:               time.Now(),
		FirstParty:              true,
		BindingRefreshAttemptID: attemptID,
		ReauthenticateClientID:  reauthenticateClientID,
		WorkloadReconnect:       reconnect,
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
	idpURL := federatedURL
	if idpURL == nil {
		callbackURL, err := endpoint.IDPCallbackURL(s.outboundOrigin().String())
		if err != nil {
			s.metrics.RecordOAuthFlowFailed(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug, mcpmetrics.OAuthFlowStageAuthorize)
			return oops.E(oops.CodeUnexpected, err, "build IDP callback URL").LogError(ctx, logger)
		}
		idpURL, err = s.identityResolver.BuildAuthorizationURL(ctx, identity.AuthorizationURLParams{
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
	}
	// Claim only after all validation and login setup succeeded, so a failed
	// preparation does not consume the dashboard's one-time decision.
	if attemptID != "" {
		if err := s.bindingRefresh.Start(ctx, attemptID, endpoint.OrganizationID, endpoint.ProjectID, endpoint.UserSessionIssuerID); err != nil {
			_, _ = s.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+challengeState.ID)
			return fmt.Errorf("start attachment refresh: %w", err)
		}
	}
	if federatedURL != nil {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
	}

	logger.InfoContext(ctx, "started first-party connect flow", attr.SlogOAuthFlowID(flowID))
	http.Redirect(w, r, idpURL.String(), http.StatusFound)
	return nil
}

func (s *Service) SetBindingRefreshAttempts(attempts *remotesessions.BindingRefreshAttempts) {
	s.bindingRefresh = attempts
}

// firstPartyBrowserError offers a dashboard recovery action only for a known
// first-party entry point or a challenge loaded from trusted flow state.
func (s *Service) firstPartyBrowserError(err error) error {
	if err == nil {
		return nil
	}
	err = oops.WithBrowserClient(err, "Speakeasy dashboard")
	if s.siteURL == nil {
		return fmt.Errorf("recover dashboard connection: %w", err)
	}
	return fmt.Errorf("recover dashboard connection: %w", oops.WithBrowserRecovery(err, &oauthui.RecoveryLink{
		URL:   s.siteURL.JoinPath("@self").String(),
		Label: "Return to dashboard",
	}))
}
