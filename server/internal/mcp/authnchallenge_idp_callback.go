// IDP callback handler for the issuer-gated authn-challenge surface.
// Pairs with remote_login_callback (in remotesessions/) — the other
// callback on this surface, used for upstream OAuth resource providers
// (Linear, Notion, etc.). Reading the two side-by-side: IDP returns user
// identity; remote returns resource-access tokens.

package mcp

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// HandleIDPCallback is the GET endpoint the IDP redirects back to after the
// user authenticates on the private-toolset path. Mounted at
// `GET /mcp/idp_callback`; the legacy `GET /mcp/{mcpSlug}/idp_callback`
// route is still accepted, but the toolset is resolved from the stored
// AuthnChallengeState.
//
// It drives the IDP wire calls through s.identityResolver (WorkOS-backed)
// and runs the standard user bootstrap (UpsertUser, posthog signup, WorkOS
// membership sync).
//
// Side effects on success: UpsertUser, AuthnChallengeState rewrite (subject
// stamped). The IDP tokens are consumed and discarded; no chat session
// persists.
func (s *Service) HandleIDPCallback(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	routeMcpSlug := chi.URLParam(r, "mcpSlug")
	logger := s.logger

	q := r.URL.Query()
	stateID := q.Get("state")
	if stateID == "" {
		return oops.E(oops.CodeBadRequest, nil, "state is required").Log(ctx, logger)
	}

	// Atomic GETDEL: the IDP-returned state URL is single-use. The fresh
	// state ID we mint below rotates the cache key, so an attacker who
	// has somehow obtained the original stateID (referrer leakage, browser
	// history sync, proxy logs) can't replay it through this handler to
	// substitute their own Subject on the victim's in-flight challenge.
	challengeState, err := s.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").Log(ctx, logger)
	}

	mcpSlug := challengeState.Endpoint.McpSlug
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "mcp slug is missing from authn challenge state").Log(ctx, logger)
	}
	if routeMcpSlug != "" && routeMcpSlug != mcpSlug {
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge state does not match this MCP server").Log(ctx, logger)
	}

	endpoint, err := s.loadResolvedMcpEndpointByRef(ctx, challengeState.Endpoint)
	if err != nil {
		return err
	}

	logger = endpoint.LogWith(s.logger).With(attr.SlogOAuthFlowID(challengeState.FlowID))
	issuerID := endpoint.UserSessionIssuerID.String()
	// mcpSlug was set from the cached ref above; re-point it at the resolved
	// endpoint's canonical slug for the flow-metric dimension.
	mcpSlug = endpoint.Slug

	// If the IDP returned an error (user cancelled at the IDP, IDP refused
	// to authenticate, etc.) per OAuth 2.0, forward it back to the MCP
	// client with the same error code so the client can render an
	// appropriate message instead of seeing a generic "state and code are
	// required" 400.
	if idpErr := q.Get("error"); idpErr != "" {
		errDescription := q.Get("error_description")
		// access_denied is the user opting out at the IDP — a decline, not an
		// errant config. Any other IDP error code (server_error, invalid_scope,
		// ...) points at IDP/config trouble. Both are terminal; bucket them
		// accordingly before bouncing the error back to the client.
		if idpErr == "access_denied" {
			s.metrics.RecordOAuthFlowDeclined(ctx, issuerID, mcpSlug, oauthFlowStageIDPCallback)
			logger.InfoContext(ctx, "oauth flow declined at idp", attr.SlogOAuthError(idpErr), attr.SlogOAuthErrorDescription(errDescription))
		} else {
			s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, oauthFlowStageIDPCallback)
			logger.InfoContext(ctx, "oauth flow failed at idp callback", attr.SlogOAuthError(idpErr), attr.SlogOAuthErrorDescription(errDescription))
		}
		clientRedirect := buildClientRedirect(challengeState.RedirectURI, "", challengeState.State, idpErr, errDescription)
		http.Redirect(w, r, clientRedirect, http.StatusFound)
		return nil
	}

	code := q.Get("code")
	if code == "" {
		// IDP returned neither code nor error — a broken IDP redirect.
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, oauthFlowStageIDPCallback)
		return oops.E(oops.CodeBadRequest, nil, "code is required").Log(ctx, logger)
	}

	// Exchange the authorization code for user identity via WorkOS.
	idpUser, err := s.identityResolver.ExchangeCodeForTokens(ctx, code)
	if err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, oauthFlowStageIDPCallback)
		return oops.E(oops.CodeUnauthorized, err, "failed to exchange IDP code").Log(ctx, logger)
	}

	// Run the standard post-IDP user bootstrap: UpsertUser + posthog
	// signup event + WorkOS membership sync. Same side effects the
	// session manager runs on dashboard logins.
	gramUserID, err := s.identityResolver.UpsertUserFromIDP(ctx, idpUser)
	if err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, oauthFlowStageIDPCallback)
		return oops.E(oops.CodeUnexpected, err, "failed to bootstrap user").Log(ctx, logger)
	}

	// Validate the user belongs to the endpoint's organization before
	// issuing a token. The mcp:connect RBAC policy operates at org level;
	// this is the first gate. The user wanted in but policy refused — a
	// config-relevant failure (e.g. the toolset is exposed to the wrong
	// audience), not a user decline.
	if _, _, ok := s.identityResolver.HasAccessToOrganization(ctx, endpoint.OrganizationID, gramUserID); !ok {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, oauthFlowStageIDPCallback)
		return oops.E(oops.CodeForbidden, nil, "user is not a member of this MCP server's organization").Log(ctx, logger)
	}

	// Mint a fresh state ID so the /connect URL we redirect to is NOT the
	// same value that just bounced through the IDP. The IDP-returned state
	// is consumed; the new ID is what /connect's GetAndDelete will burn.
	subject := urn.NewUserSubject(gramUserID)
	// Rotate only the cache-key ID (replay protection). challengeState.FlowID
	// is deliberately left untouched so the flow stays correlatable across the
	// rotation — do not regenerate it here.
	challengeState.ID = uuid.NewString()
	challengeState.Subject = &subject
	if err := s.authnChallengeCache.Store(ctx, challengeState); err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, oauthFlowStageIDPCallback)
		return oops.E(oops.CodeUnexpected, err, "failed to update authn challenge state").Log(ctx, logger)
	}

	// challengeState.Endpoint.BaseURL was stamped at mint time. New
	// mints always populate it; the IDP callback can rebuild the
	// consent redirect from cache alone without a fresh custom_domains
	// lookup (the callback is registered at a global URL and loses the
	// request's customdomains.Context). Empty value falls back to the
	// server default for in-flight states minted before this field
	// landed.
	baseURL := challengeState.Endpoint.BaseURL
	if baseURL == "" {
		baseURL = s.serverURL.String()
	}
	consentURL, err := endpoint.ConsentURL(baseURL, challengeState.ID)
	if err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, oauthFlowStageIDPCallback)
		return oops.E(oops.CodeUnexpected, err, "build consent URL").Log(ctx, logger)
	}
	http.Redirect(w, r, consentURL, http.StatusFound)
	return nil
}
