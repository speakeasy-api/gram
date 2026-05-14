// IDP callback handler for the issuer-gated authn-challenge surface.
// Pairs with the to-be-implemented remote_login_callback — the other
// callback on this surface, used for upstream OAuth resource providers
// (Linear, Notion, etc.). Reading the two side-by-side: IDP returns user
// identity; remote returns resource-access tokens.

package mcp

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	customdomains_repo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
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

	toolset, err := s.resolveMcp(ctx, challengeState.Endpoint)
	switch {
	case errors.Is(err, errToolsetNotFound):
		return oops.E(oops.CodeNotFound, err, "mcp server not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "failed to load MCP server").Log(ctx, logger)
	}
	if !toolset.UserSessionIssuerID.Valid {
		return oops.E(oops.CodeNotFound, nil, "not found")
	}
	if err := s.requireUserSessionIssuer(ctx, toolset); err != nil {
		return err
	}

	logger = s.logger.With(
		attr.SlogToolsetID(toolset.ID.String()),
		attr.SlogProjectID(toolset.ProjectID.String()),
	)

	// If the IDP returned an error (user cancelled at the IDP, IDP refused
	// to authenticate, etc.) per OAuth 2.0, forward it back to the MCP
	// client with the same error code so the client can render an
	// appropriate message instead of seeing a generic "state and code are
	// required" 400.
	if idpErr := q.Get("error"); idpErr != "" {
		errDescription := q.Get("error_description")
		clientRedirect := buildClientRedirect(challengeState.RedirectURI, "", challengeState.State, idpErr, errDescription)
		http.Redirect(w, r, clientRedirect, http.StatusFound)
		return nil
	}

	code := q.Get("code")
	if code == "" {
		return oops.E(oops.CodeBadRequest, nil, "code is required").Log(ctx, logger)
	}

	// Exchange the authorization code for user identity via WorkOS.
	idpUser, err := s.identityResolver.ExchangeCodeForTokens(ctx, code)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "failed to exchange IDP code").Log(ctx, logger)
	}

	// Run the standard post-IDP user bootstrap: UpsertUser + posthog
	// signup event + WorkOS membership sync. Same side effects the
	// session manager runs on dashboard logins.
	gramUserID, err := s.identityResolver.UpsertUserFromIDP(ctx, idpUser)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to bootstrap user").Log(ctx, logger)
	}

	// Validate the user belongs to the toolset's organization before
	// issuing a token. The mcp:connect RBAC policy operates at org level;
	// this is the first gate.
	if _, _, ok := s.identityResolver.HasAccessToOrganization(ctx, toolset.OrganizationID, gramUserID); !ok {
		return oops.E(oops.CodeForbidden, nil, "user is not a member of this MCP server's organization").Log(ctx, logger)
	}

	// Mint a fresh state ID so the /connect URL we redirect to is NOT the
	// same value that just bounced through the IDP. The IDP-returned state
	// is consumed; the new ID is what /connect's GetAndDelete will burn.
	subject := urn.NewUserSubject(gramUserID)
	challengeState.ID = uuid.NewString()
	challengeState.Subject = &subject
	if err := s.authnChallengeCache.Store(ctx, challengeState); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to update authn challenge state").Log(ctx, logger)
	}

	baseURL := s.serverURL.String()
	if challengeState.Endpoint.CustomDomainID.Valid {
		domain, derr := customdomains_repo.New(s.db).GetCustomDomainByIDAndOrganization(ctx, customdomains_repo.GetCustomDomainByIDAndOrganizationParams{
			ID:             challengeState.Endpoint.CustomDomainID.UUID,
			OrganizationID: toolset.OrganizationID,
		})
		switch {
		case derr == nil:
			baseURL = fmt.Sprintf("https://%s", domain.Domain)
		case !errors.Is(derr, pgx.ErrNoRows):
			return oops.E(oops.CodeUnexpected, derr, "failed to load custom domain").Log(ctx, logger)
		}
	}
	consentURL := fmt.Sprintf("%s/mcp/%s/connect?state=%s", baseURL, mcpSlug, url.QueryEscape(challengeState.ID))
	http.Redirect(w, r, consentURL, http.StatusFound)
	return nil
}
