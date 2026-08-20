// Non-consuming per-card actions for the consent page: connect (start the
// upstream OAuth leg carrying the auto-refresh choice), refresh (renew the
// external service now), disconnect (soft-delete the subject's
// remote_session), and set_auto_refresh (persist the preference). Unlike the
// approve/deny POST, these read the challenge state with a plain Get — the
// page stays usable and the single consuming GetAndDelete transition remains
// the approve/deny handler's alone, so at most one client grant is ever minted
// per authorization request.

package mcp

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

// HandleConsentAction serves `POST /mcp/{mcpSlug}/connect/remote-session`.
func (s *Service) HandleConsentAction(w http.ResponseWriter, r *http.Request) error {
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
	return s.ServeConsentAction(w, r, endpoint)
}

// ServeConsentAction is the post-resolution handler, shared with /x/mcp.
func (s *Service) ServeConsentAction(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) error {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)

	if r.Method != http.MethodPost {
		return oops.E(oops.CodeBadRequest, nil, "method not allowed").LogError(ctx, logger)
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to parse form").LogError(ctx, logger)
	}

	stateID := r.PostForm.Get("state")
	if stateID == "" {
		return oops.E(oops.CodeBadRequest, nil, "state is required").LogError(ctx, logger)
	}

	// Plain Get: card actions are idempotent within the challenge TTL and
	// must not consume the state the approve POST needs.
	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").LogError(ctx, logger)
	}
	logger = logger.With(attr.SlogOAuthFlowID(challengeState.FlowID))
	if err := endpoint.ValidateRef(challengeState.Endpoint); err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state does not match this MCP server").LogError(ctx, logger)
	}
	if challengeState.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf_token")), []byte(challengeState.CSRFToken)) != 1 {
		return oops.E(oops.CodeUnauthorized, nil, "invalid consent csrf token").LogError(ctx, logger)
	}
	if challengeState.Subject == nil || challengeState.Subject.IsZero() {
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge subject is not resolved").LogError(ctx, logger)
	}
	subject := *challengeState.Subject

	clients, err := s.remoteChallengeMgr.ListClients(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list remote session clients").LogError(ctx, logger)
	}

	// The posted client_id is only a lookup key; the acted-on client is
	// re-resolved through the endpoint's current bindings so a crafted form
	// cannot reach a client this MCP server is not bound to. The page-level
	// set_auto_refresh action targets every bound client and posts none.
	resolveClient := func() (*remotesessions.Client, error) {
		clientID, perr := uuid.Parse(r.PostForm.Get("client_id"))
		if perr != nil {
			return nil, oops.E(oops.CodeBadRequest, perr, "invalid client_id").LogError(ctx, logger)
		}
		for i := range clients {
			if clients[i].ID == clientID {
				return &clients[i], nil
			}
		}
		return nil, oops.E(oops.CodeBadRequest, nil, "unknown remote session client for this MCP server").LogError(ctx, logger)
	}

	backURL := fmt.Sprintf("/%s/%s/connect?state=%s", endpoint.RouteBase, endpoint.Slug, url.QueryEscape(stateID))

	// Auto refresh is only the subject's choice while the organization lets
	// them choose. Under either managed policy the posted value is ignored, so
	// a crafted form can neither disable a required connection nor opt into
	// refresh the organization disabled.
	autoRefreshPolicy := s.resolveAutoRefreshPolicy(ctx, endpoint.OrganizationID)

	switch r.PostForm.Get("action") {
	case "connect":
		client, cerr := resolveClient()
		if cerr != nil {
			return cerr
		}
		// Only a user-controlled policy authors a stored preference. Managed
		// policies leave it unset so remote_sessions.auto_refresh stays purely
		// user-originated: the keepalive applies the policy at query time, so
		// persisting a forced value would buy nothing and would overwrite the
		// choice a subject made before the organization took over.
		var autoRefresh *bool
		if autoRefreshPolicy == autoRefreshUserControlled {
			if _, posted := r.PostForm["auto_refresh"]; posted {
				v := r.PostForm.Get("auto_refresh") == "on"
				autoRefresh = &v
			}
		}
		challengeURL, berr := s.remoteChallengeMgr.BuildAuthorizationUrl(ctx, remotesessions.ParentChallenge{
			ID:                  challengeState.ID,
			ProjectID:           endpoint.ProjectID,
			OrganizationID:      endpoint.OrganizationID,
			UserSessionIssuerID: endpoint.UserSessionIssuerID,
			Subject:             challengeState.Subject,
			McpSlug:             endpoint.Slug,
			RouteBase:           endpoint.RouteBase,
			FinalRedirectURI:    "",
			Resource:            endpoint.UpstreamResource,
			AutoRefresh:         autoRefresh,
		}, *client)
		if berr != nil {
			return oops.E(oops.CodeUnexpected, berr, "build authorization url").LogError(ctx, logger)
		}
		http.Redirect(w, r, challengeURL, http.StatusSeeOther)
		return nil

	case "disconnect":
		client, cerr := resolveClient()
		if cerr != nil {
			return cerr
		}
		if _, err := s.remoteChallengeMgr.DisconnectRemoteSession(ctx, subject, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, client.ID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "disconnect remote session").LogError(ctx, logger)
		}
		http.Redirect(w, r, backURL, http.StatusSeeOther)
		return nil

	case "refresh":
		client, cerr := resolveClient()
		if cerr != nil {
			return cerr
		}
		result, refreshErr := s.remoteChallengeMgr.RefreshRemoteSession(
			ctx,
			subject,
			endpoint.ProjectID,
			endpoint.OrganizationID,
			endpoint.UserSessionIssuerID,
			client.ID,
			endpoint.UpstreamResource,
		)
		if refreshErr != nil {
			switch {
			case errors.Is(refreshErr, remotesessions.ErrRemoteSessionNotRefreshable):
				return oops.E(oops.CodeBadRequest, refreshErr, "Reconnect this service before refreshing it.").LogWarn(ctx, logger)
			default:
				var tokenRefreshErr *remotesessions.TokenRefreshError
				if errors.As(refreshErr, &tokenRefreshErr) {
					return oops.E(oops.CodeBadRequest, refreshErr, "Unable to refresh: %s", tokenRefreshErr.Reason).LogWarn(ctx, logger)
				}
				return oops.E(oops.CodeUnexpected, refreshErr, "refresh remote session").LogError(ctx, logger)
			}
		}
		if result.Outcome == remotesessions.RefreshOutcomeSessionInactive {
			return oops.E(oops.CodeBadRequest, nil, "Reconnect this service before refreshing it.").LogWarn(ctx, logger)
		}
		http.Redirect(w, r, backURL, http.StatusSeeOther)
		return nil

	case "set_auto_refresh":
		// Page-level "Auto refresh": persist the choice for every bound client.
		// Clients without a stored session update zero rows; their preference
		// rides the next connect instead. Managed policies ignore this action:
		// their effective value comes from organization policy, while the stored
		// user preference remains intact if the organization later restores
		// user-controlled refresh.
		if autoRefreshPolicy != autoRefreshUserControlled {
			http.Redirect(w, r, backURL, http.StatusSeeOther)
			return nil
		}
		enabled := r.PostForm.Get("auto_refresh") == "on"
		for i := range clients {
			if _, err := s.remoteChallengeMgr.SetRemoteSessionAutoRefresh(ctx, subject, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, clients[i].ID, enabled); err != nil {
				return oops.E(oops.CodeUnexpected, err, "set remote session auto refresh").LogError(ctx, logger)
			}
		}
		http.Redirect(w, r, backURL, http.StatusSeeOther)
		return nil

	default:
		return oops.E(oops.CodeBadRequest, nil, `action must be "connect", "refresh", "disconnect", or "set_auto_refresh"`).LogError(ctx, logger)
	}
}
