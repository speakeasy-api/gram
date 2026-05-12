// Speakeasy IDP callback handler for the issuer-gated authn-challenge
// surface. Pairs with the to-be-implemented remote_login_callback — the
// other callback on this surface, used for upstream OAuth resource
// providers (Linear, Notion, etc.). Reading the two side-by-side: IDP
// returns user identity; remote returns resource-access tokens.

package mcp

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// HandleIDPCallback is the GET endpoint Speakeasy IDP redirects back to
// after the user authenticates on the private-toolset path. Mounted at
// `GET /mcp/{mcpSlug}/idp_callback`.
//
// It is independent of the chat-session manager: we drive the IDP wire calls
// directly through s.idpClient (see speakeasyclient.go) and skip everything
// the chat-session path bundles in (userInfoCache writes, posthog, pylon,
// WorkOS sync, admin override, cookie issuance). We DO upsert the Gram user
// row -- otherwise we have no Gram user_id to put in the URN.
//
// Side effects on success: UpsertUser, AuthnChallengeState rewrite (subject
// stamped). The IDP idToken is consumed and discarded; no chat session
// persists.
func (s *Service) HandleIDPCallback(w http.ResponseWriter, r *http.Request) error {
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

	q := r.URL.Query()
	stateID := q.Get("state")
	code := q.Get("code")
	if stateID == "" || code == "" {
		return oops.E(oops.CodeBadRequest, nil, "state and code are required").Log(ctx, logger)
	}

	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").Log(ctx, logger)
	}

	// State-confusion guard: the state must belong to this toolset.
	if challengeState.ToolsetID != toolset.ID {
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge state does not match this MCP server").Log(ctx, logger)
	}

	idToken, err := s.idpClient.ExchangeCode(ctx, code)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "failed to exchange IDP code").Log(ctx, logger)
	}

	validated, err := s.idpClient.ValidateIDToken(ctx, idToken)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "failed to validate IDP id token").Log(ctx, logger)
	}

	// Here we validate that the owner belongs to the toolset Org before proceeding
	// We don't want to mess around with issuing tokens to non-org users
	// Why not the project? Well the mcp:connect RBAC policy operates at
	// an organization level. This policy will be enforced in the MCP endpoint
	// but we defer the check to be more general here
	authorized := false
	for _, org := range validated.Organizations {
		if org.ID == toolset.OrganizationID {
			authorized = true
			break
		}
	}
	if !authorized {
		return oops.E(oops.CodeForbidden, nil, "user is not a member of this MCP server's organization").Log(ctx, logger)
	}

	// Run the shared post-IDP user bootstrap: UpsertUser + posthog signup
	// event + WorkOS membership sync. Same side effects the chat-session
	// manager runs on dashboard logins, identical ordering. WorkOS sync in
	// particular is required so downstream RBAC has the right org-membership
	// records for an MCP-only user authenticating for the first time.
	user, err := s.idpClient.BootstrapUser(ctx, validated)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to bootstrap user").Log(ctx, logger)
	}

	subject := urn.NewUserSubject(user.ID)
	challengeState.Subject = &subject
	if err := s.authnChallengeCache.Store(ctx, challengeState); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to update authn challenge state").Log(ctx, logger)
	}

	baseURL := s.serverURL.String()
	if customDomainCtx != nil {
		baseURL = fmt.Sprintf("https://%s", customDomainCtx.Domain)
	}
	consentURL := fmt.Sprintf("%s/mcp/%s/connect?state=%s", baseURL, mcpSlug, url.QueryEscape(challengeState.ID))
	http.Redirect(w, r, consentURL, http.StatusFound)
	return nil
}
