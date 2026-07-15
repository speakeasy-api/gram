package mcp

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ServeFirstPartyConnect is the dashboard's entry point for establishing the
// upstream remote_sessions an issuer-gated MCP server needs. It mints a
// first-party authn challenge and bounces through the gram server's own IDP
// login — the same flow a real MCP client runs via /x/mcp/{slug}/authorize —
// rather than borrowing the dashboard's gram_session.
//
// This is deliberately decoupled from the dashboard session: the subject is
// stamped onto the challenge by HandleIDPCallback from authoritative IDP
// claims, and the only state is the challenge in Redis keyed by the OIDC
// `state` param. Nothing here reads, sets, or clears a cookie, so opening this
// page can never touch the dashboard's session (and the org-membership gate in
// HandleIDPCallback enforces access once the IDP identifies the user).
//
// No ClientID/RedirectURI: a first-party challenge has no MCP client to grant
// to or redirect back to. Once the user links a card on the consent page, the
// flow is terminal and the page closes its dashboard-opened tab.
func (s *Service) ServeFirstPartyConnect(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) error {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)

	csrfToken, err := generateOpaqueToken()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "generate consent csrf token").LogError(ctx, logger)
	}

	baseURL := s.BaseURLForRequest(r)
	flowID := uuid.NewString()
	challengeID := uuid.NewString()
	challengeState := AuthnChallengeState{
		ID:                  challengeID,
		FlowID:              flowID,
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		Endpoint:            endpoint.EndpointRef(baseURL),
		ClientID:            "",
		RedirectURI:         "",
		State:               "",
		CodeChallenge:       "",
		CodeChallengeMethod: "",
		CSRFToken:           csrfToken,
		// Subject is stamped by HandleIDPCallback from authoritative IDP claims.
		Subject:    nil,
		CreatedAt:  time.Now(),
		FirstParty: true,
	}
	if err := s.authnChallengeCache.Store(ctx, challengeState); err != nil {
		return oops.E(oops.CodeUnexpected, err, "store authn challenge state").LogError(ctx, logger)
	}

	s.metrics.RecordOAuthFlowStarted(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug)

	callbackURL, err := endpoint.IDPCallbackURL(s.serverURL.String())
	if err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug, oauthFlowStageAuthorize)
		return oops.E(oops.CodeUnexpected, err, "build IDP callback URL").LogError(ctx, logger)
	}
	idpURL, err := s.identityResolver.BuildAuthorizationURL(ctx, identity.AuthorizationURLParams{
		CallbackURL:     callbackURL,
		Scope:           "",
		State:           challengeID,
		ScopesSupported: nil,
	})
	if err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug, oauthFlowStageAuthorize)
		return oops.E(oops.CodeUnexpected, err, "build IDP authorization URL").LogError(ctx, logger)
	}

	logger.InfoContext(ctx, "started first-party connect flow", attr.SlogOAuthFlowID(flowID))
	http.Redirect(w, r, idpURL.String(), http.StatusFound)
	return nil
}
