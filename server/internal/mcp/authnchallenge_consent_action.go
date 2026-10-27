// Per-card consent actions do not consume challenges. Delegation retry consumes
// and rotates the challenge; approve/deny remain the only grant-minting actions.

package mcp

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/hiddenmcpservers"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// consentActionFormMaxBytes caps a consent card action's form body. The form
// carries a handful of short fields; 16 KiB leaves room without letting one
// request hold a large buffer.
const consentActionFormMaxBytes = 16 << 10 // 16 KiB

// consentActionFormErrorKey retains the first parse failure when the route
// wrapper reads the action to choose a response format. ParseForm caches values
// but does not repeat its error on subsequent calls.
type consentActionFormErrorKey struct{}

func parseConsentActionForm(w http.ResponseWriter, r *http.Request) error {
	if err, ok := r.Context().Value(consentActionFormErrorKey{}).(error); ok {
		return err
	}
	r.Body = http.MaxBytesReader(w, r.Body, consentActionFormMaxBytes)
	if err := r.ParseForm(); err != nil {
		return fmt.Errorf("parse consent action form: %w", err)
	}
	return nil
}

// HandleConsentAction serves `POST /mcp/{mcpSlug}/connect/remote-session`.
func (s *Service) HandleConsentAction(w http.ResponseWriter, r *http.Request) error {
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
	return s.ServeConsentAction(w, r, endpoint)
}

// ServeConsentAction is the post-resolution handler.
func (s *Service) ServeConsentAction(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) (retErr error) {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)

	if r.Method != http.MethodPost {
		return oops.E(oops.CodeBadRequest, nil, "method not allowed").LogError(ctx, logger)
	}
	if err := parseConsentActionForm(w, r); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to parse form").LogError(ctx, logger)
	}

	stateID := r.PostForm.Get("state")
	if stateID == "" {
		return fmt.Errorf("validate consent request: %w", oops.WithBrowserReason(oops.E(oops.CodeBadRequest, nil, "state is required").LogError(ctx, logger), oauthui.ReasonInvalidLink))
	}

	// Plain Get: card actions are idempotent within the challenge TTL and
	// must not consume the state the approve POST needs.
	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return consentChallengeCacheError(ctx, logger, err)
	}
	if challengeState.FirstParty {
		defer func() { retErr = s.firstPartyBrowserError(retErr) }()
	}
	if err := validateChallengeBrowser(r, challengeState, false); err != nil {
		return fmt.Errorf("validate consent request: %w", oops.WithBrowserReason(oops.E(oops.CodeBadRequest, err, "invalid consent browser binding"), oauthui.ReasonSessionChanged))
	}
	logger = logger.With(attr.SlogOAuthFlowID(challengeState.FlowID))
	if err := endpoint.ValidateChallenge(ctx, challengeState.Endpoint, challengeState.UserSessionIssuerID); err != nil {
		if errors.Is(err, networkingress.ErrAuthorityUnavailable) {
			s.metrics.RecordOAuthAuthorityUnavailable(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug, mcpmetrics.OAuthFlowStageConsent)
		}
		return oauthAuthorityError(err).LogError(ctx, logger)
	}
	if challengeState.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf_token")), []byte(challengeState.CSRFToken)) != 1 {
		return oops.E(oops.CodeBadRequest, nil, "invalid consent csrf token").LogWarn(ctx, logger)
	}
	if challengeState.Subject == nil || challengeState.Subject.IsZero() {
		return oops.E(oops.CodeBadRequest, nil, "authn challenge subject is not resolved").LogWarn(ctx, logger)
	}
	// Revalidate private authority before retry consumes state or card actions
	// read or mutate credentials; the resolved endpoint can already be stale.
	if err := endpoint.ValidateLiveChallenge(ctx, s.db, challengeState.Endpoint); err != nil {
		if errors.Is(err, networkingress.ErrAuthorityUnavailable) {
			s.metrics.RecordOAuthAuthorityUnavailable(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug, mcpmetrics.OAuthFlowStageConsent)
		}
		return oauthAuthorityError(err).LogError(ctx, logger)
	}
	action := r.PostForm.Get("action")
	if endpoint.MetaMcpServerID.Valid && (action == "identity_chaining_check" || action == "validate") {
		lockedDown, err := s.endpointCustomDomainLockdown(ctx, logger, endpoint)
		if err != nil {
			return err
		}
		if lockedDown {
			return oops.E(oops.CodeForbidden, nil, "Start authorization from this gateway's custom domain to approve access.").LogWarn(ctx, logger)
		}
	}
	subject := *challengeState.Subject

	switch r.PostForm.Get("action") {
	case "delegate_grant", "finish_delegation":
		return s.servePostLoginDelegationAction(w, r, endpoint, challengeState)
	case "agent_connections", "agent_attach", "agent_detach":
		return s.serveConsentAgentConnections(w, r, endpoint, challengeState)
	case "retry_delegation":
		return s.retryFederatedDelegation(w, r, endpoint, challengeState)
	case "identity_chaining_check":
		return s.serveIdentityChainingCheck(w, r, endpoint, challengeState)
	case "hide_server", "show_server":
		return s.serveConsentHideServer(w, r, endpoint, challengeState, stateID)
	}

	bound, err := s.listEndpointClients(ctx, endpoint, challengeState)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list remote session clients").LogError(ctx, logger)
	}
	clients := subjectConnectedClients(bound)

	// The posted client_id is only a lookup key; the acted-on client is
	// re-resolved through the endpoint's current bindings so a crafted form
	// cannot reach a client this MCP server is not bound to. The page-level
	// set_auto_refresh action targets every bound client and posts none.
	resolveClient := func() (*remotesessions.Client, error) {
		clientID, perr := uuid.Parse(r.PostForm.Get("client_id"))
		if perr != nil {
			return nil, oops.E(oops.CodeBadRequest, perr, "invalid client_id").LogError(ctx, logger)
		}
		if client := findConsentClient(clients, clientID); client != nil {
			return client, nil
		}
		return nil, oops.E(oops.CodeBadRequest, nil, "unknown remote session client for this MCP server").LogError(ctx, logger)
	}

	// A card on a gateway server's connect page posts that server, so every
	// action returns the user to the page they acted on.
	focusServer := r.PostForm.Get("server")
	if len(focusServer) > maxFocusServerLength {
		return oops.E(oops.CodeBadRequest, nil, "server is too long").LogError(ctx, logger)
	}
	backURL := endpoint.consentPath() + "?state=" + url.QueryEscape(stateID)
	if focusServer != "" {
		backURL += "&server=" + url.QueryEscape(focusServer)
	}

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
		challengeURL, hop, berr := s.buildRemoteConnectURL(ctx, logger, endpoint, challengeState, *client, bound, autoRefresh, focusServer)
		if berr != nil {
			return berr
		}
		if hop {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Referrer-Policy", "no-referrer")
		}
		http.Redirect(w, r, challengeURL, http.StatusSeeOther)
		return nil

	case "disconnect":
		client, cerr := resolveClient()
		if cerr != nil {
			return cerr
		}
		if _, err := s.remoteChallengeMgr.DisconnectRemoteSession(ctx, subject, endpoint.clientProjectID(*client), endpoint.OrganizationID, endpoint.UserSessionIssuerID, client.ID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "disconnect remote session").LogError(ctx, logger)
		}
		// Latch auto-connect off. Without this the redirect back to the consent
		// page would see a single disconnected card and immediately bounce the
		// user into the provider again, making disconnect impossible to
		// complete.
		//
		// CompareAndSwap, not Store: this handler read the challenge with a
		// plain Get, so the approve POST may have consumed it since. A Store
		// would put the consumed challenge back and let a replayed approval
		// mint a second grant against it. A lost swap means the challenge
		// moved on, which is not a reason to fail the disconnect that already
		// succeeded.
		if !challengeState.AutoConnectDone {
			latched := challengeState
			latched.AutoConnectDone = true
			if _, err := s.authnChallengeCache.CompareAndSwap(ctx, challengeState, latched); err != nil {
				logger.WarnContext(ctx, "latch auto-connect off after disconnect", attr.SlogError(err))
			}
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
			endpoint.clientProjectID(*client),
			endpoint.OrganizationID,
			endpoint.UserSessionIssuerID,
			client.ID,
		)
		if refreshErr != nil {
			switch {
			case errors.Is(refreshErr, remotesessions.ErrRemoteSessionNotRefreshable):
				return oops.E(oops.CodeBadRequest, refreshErr, "Reconnect this service before refreshing it.").LogWarn(ctx, logger)
			default:
				if tokenRefreshErr, ok := errors.AsType[*remotesessions.TokenRefreshError](refreshErr); ok {
					return oops.E(oops.CodeBadRequest, refreshErr, "Unable to refresh: %s", tokenRefreshErr.Reason).LogWarn(ctx, logger)
				}
				return oops.E(oops.CodeUnexpected, refreshErr, "refresh remote session").LogError(ctx, logger)
			}
		}
		if result.Outcome == remotesessionmetrics.RefreshOutcomeSessionInactive {
			return oops.E(oops.CodeBadRequest, nil, "Reconnect this service before refreshing it.").LogWarn(ctx, logger)
		}
		http.Redirect(w, r, backURL, http.StatusSeeOther)
		return nil

	case "validate":
		client, cerr := resolveClient()
		if cerr != nil {
			return cerr
		}
		err := s.validateRemoteSession(ctx, logger, endpoint, challengeState, *client)
		switch {
		case errors.Is(err, errValidationRateLimited):
			http.Redirect(w, r, backURL+"&validate_limited="+url.QueryEscape(client.ID.String()), http.StatusSeeOther)
			return nil
		case err != nil:
			return err
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
			if _, err := s.remoteChallengeMgr.SetRemoteSessionAutoRefresh(ctx, subject, endpoint.clientProjectID(clients[i]), endpoint.OrganizationID, endpoint.UserSessionIssuerID, clients[i].ID, enabled); err != nil {
				return oops.E(oops.CodeUnexpected, err, "set remote session auto refresh").LogError(ctx, logger)
			}
		}
		http.Redirect(w, r, backURL, http.StatusSeeOther)
		return nil

	default:
		return oops.E(oops.CodeBadRequest, nil, `action must be "connect", "refresh", "validate", "disconnect", "set_auto_refresh", "retry_delegation", or "identity_chaining_check"`).LogError(ctx, logger)
	}
}

// clientServesHostedMember reports whether a hosted member the consent
// subject is admitted to authenticates through client.
func (s *Service) clientServesHostedMember(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, client remotesessions.Client) (bool, error) {
	members, err := s.consentSubjectMembers(ctx, endpoint, challengeState)
	if err != nil {
		return false, err
	}
	for _, member := range members {
		if member.backend != metaMemberBackendProxied && memberUsesClient(member, client.ID, client.RemoteSessionIssuerID, client.ProjectID) {
			return true, nil
		}
	}
	return false, nil
}

// buildRemoteConnectURL builds the upstream authorization URL for one remote
// session client. Shared by the page's explicit Connect action and by the
// consent page's auto-connect, so both legs resolve the upstream resource
// identically — an auto-connect that qualified the credential differently from
// a manual one would mint a session the runtime then rejects. hop reports a
// URL to the remote login browser hop rather than the upstream provider.
// bound is every client bound to the endpoint, self clients included, since
// each one's upstream decides what client may claim.
func (s *Service) buildRemoteConnectURL(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	client remotesessions.Client,
	bound []remotesessions.Client,
	autoRefresh *bool,
	focusServer string,
) (string, bool, error) {
	// endpoint.UpstreamResource only when no other bound client can claim
	// it: under multi-binding it may belong to a different client's
	// upstream, while a client shared by servers with different upstreams
	// derives none on its own and would record a grant nothing routes to.
	var clientResource string
	var rerr error
	claimedByMember := false
	resourceMember := chosenMetaMember{serverID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, projectID: uuid.Nil}
	if endpoint.MetaMcpServerID.Valid {
		// Member visibility is judged against the consent subject, as the runtime
		// request the minted session will make will be.
		memberCtx, cerr := s.contextForSessionSubject(ctx, endpoint, *challengeState.Subject, "consent:"+challengeState.ID, challengeState.ClientID)
		if cerr != nil {
			return "", false, oops.E(oops.CodeUnexpected, cerr, "stamp consent subject context").LogError(ctx, logger)
		}
		clientResource, resourceMember, claimedByMember, rerr = s.resolveMetaMemberResource(memberCtx, logger, endpoint, client)
	}
	// Gate on the claim, not an empty resource: an ambiguous meta MCP has
	// decided, and falling back would qualify the credential anyway. A
	// gateway client no proxied member is associated with serves only the
	// hosted members that use it, which keep the issuer-derived path; one
	// that serves no member at all is refused rather than qualified by the
	// issuer's inventory.
	if rerr == nil && !claimedByMember && endpoint.MetaMcpServerID.Valid {
		hosted, herr := s.clientServesHostedMember(ctx, endpoint, challengeState, client)
		if herr != nil {
			return "", false, oops.E(oops.CodeUnexpected, herr, "resolve gateway members for connection").LogError(ctx, logger)
		}
		if !hosted {
			return "", false, oops.E(oops.CodeBadRequest, nil, "this connection is not configured for any server this gateway serves").LogWarn(ctx, logger)
		}
	}
	if rerr == nil && !claimedByMember {
		boundIDs := make([]uuid.UUID, 0, len(bound))
		for i := range bound {
			boundIDs = append(boundIDs, bound[i].ID)
		}
		clientResource, rerr = s.remoteChallengeMgr.ResourceForClientAtUpstream(ctx, endpoint.OrganizationID, client.ID, boundIDs, endpoint.UpstreamResource)
	}
	if rerr != nil {
		return "", false, oops.E(oops.CodeUnexpected, rerr, "derive client upstream resource").LogError(ctx, logger)
	}

	// The upstream callback returns to the consent page.
	consentURL, err := s.remoteLoginConsentURL(endpoint, challengeState)
	if err != nil {
		return "", false, oops.E(oops.CodeUnexpected, err, "build consent URL").LogError(ctx, logger)
	}
	// The login runs in the client's own project. An organization-level client
	// on an organization gateway belongs to none, so it runs in the project of
	// the member it was qualified to, whose resource the login reads.
	loginProjectID := endpoint.clientProjectID(client)
	if loginProjectID == uuid.Nil && resourceMember.serverID.Valid {
		loginProjectID = resourceMember.projectID
	}
	parent := remotesessions.ParentChallenge{
		ConnectionPopup:     challengeState.FirstParty,
		ID:                  challengeState.ID,
		ProjectID:           loginProjectID,
		OrganizationID:      endpoint.OrganizationID,
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		Subject:             challengeState.Subject,
		McpSlug:             endpoint.Slug,
		RouteBase:           endpoint.RouteBase,
		McpServerID:         endpoint.McpServerID,
		MetaMcpServerID:     endpoint.MetaMcpServerID,
		ResourceMcpServerID: resourceMember.serverID,
		FinalRedirectURI:    "",
		ConsentURL:          consentURL,
		ConsentServer:       focusServer,
		Resource:            clientResource,
		AutoRefresh:         autoRefresh,
		Authority:           challengeState.Endpoint.Authority,
		BrowserCookieID:     "",
		BrowserHash:         "",
	}
	challengeURL, hop, berr := s.startRemoteLogin(ctx, challengeState, s.remoteChallengeMgr.RemoteLoginCallbackOrigin(client), parent, func(parent remotesessions.ParentChallenge) (string, error) {
		return s.remoteChallengeMgr.BuildAuthorizationUrl(ctx, parent, client)
	})
	if errors.Is(berr, remotesessions.ErrIssuerNoAuthorizationEndpoint) {
		return "", false, oops.E(oops.CodeBadRequest, berr, "This service doesn't support signing in from Speakeasy.").LogWarn(ctx, logger)
	}
	if berr != nil {
		return "", false, oops.E(oops.CodeUnexpected, berr, "build authorization url").LogError(ctx, logger)
	}
	return challengeURL, hop, nil
}

// findConsentClient picks the endpoint's client with the given id, or nil.
func findConsentClient(clients []remotesessions.Client, id uuid.UUID) *remotesessions.Client {
	for i := range clients {
		if clients[i].ID == id {
			return &clients[i]
		}
	}
	return nil
}

// serveConsentHideServer hides or shows a gateway server the subject is admitted to.
func (s *Service) serveConsentHideServer(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, stateID string) error {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger).With(attr.SlogOAuthFlowID(challengeState.FlowID))
	if !endpoint.MetaMcpServerID.Valid {
		return oops.E(oops.CodeBadRequest, nil, "only gateway servers can be hidden").LogWarn(ctx, logger)
	}
	subject := *challengeState.Subject
	if subject.Kind != urn.SessionSubjectKindUser {
		return oops.E(oops.CodeBadRequest, nil, "only a signed-in person can hide servers").LogWarn(ctx, logger)
	}
	serverID, err := uuid.Parse(r.PostForm.Get("server_id"))
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid server_id").LogWarn(ctx, logger)
	}
	members, err := s.consentSubjectMembers(ctx, endpoint, challengeState)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "resolve gateway members").LogError(ctx, logger)
	}
	if !slices.ContainsFunc(members, func(member metaMember) bool { return member.serverID == serverID }) {
		return oops.E(oops.CodeBadRequest, nil, "unknown server for this gateway").LogWarn(ctx, logger)
	}
	if r.PostForm.Get("action") == "hide_server" {
		err = hiddenmcpservers.Hide(ctx, s.db, endpoint.OrganizationID, subject.ID, serverID)
	} else {
		err = hiddenmcpservers.Unhide(ctx, s.db, endpoint.OrganizationID, subject.ID, serverID)
	}
	switch {
	case errors.Is(err, hiddenmcpservers.ErrServerNotFound):
		return oops.E(oops.CodeBadRequest, err, "unknown server for this gateway").LogWarn(ctx, logger)
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "update hidden server").LogError(ctx, logger)
	}
	// The page script updates the list in place; a plain form post returns
	// to the page.
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	http.Redirect(w, r, endpoint.consentPath()+"?state="+url.QueryEscape(stateID), http.StatusSeeOther)
	return nil
}
