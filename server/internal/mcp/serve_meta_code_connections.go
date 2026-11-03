package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"golang.org/x/sync/errgroup"
)

var _ codemode.ConnectionBackend = (*metaCodeBackend)(nil)

// Recovery uses the current visible member set, including members not yet
// approved at consent, but never turns that visibility into tool authority.
func (b *metaCodeBackend) connectionMembers(ctx context.Context) (context.Context, *metaGateContext, []metaMember, error) {
	return b.currentState(ctx, false, true)
}

// DiscoveryServers exposes only admission-bound visible members. It reads known
// consent metadata, never resolves/refreshes upstream credentials or lists tools.
func (b *metaCodeBackend) DiscoveryServers(ctx context.Context) ([]codemode.Server, error) {
	ctx, gate, members, err := b.connectionMembers(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]codemode.Server, 0, len(members))
	for _, member := range members {
		state := b.knownConnectionState(ctx, gate, member)
		result = append(result, codemode.Server{Slug: member.slug, Name: member.name, Connection: state})
	}
	return result, nil
}

func (b *metaCodeBackend) knownConnectionState(ctx context.Context, gate *metaGateContext, member metaMember) *codemode.ConnectionState {
	state := &codemode.ConnectionState{Consent: "not_required", Authentication: "unknown", Instructions: "", CheckedAt: nil}
	if member.backend == metaMemberBackendHosted && !member.remoteSessionIssuerID.Valid {
		state.Authentication = "not_required"
	}
	if gate.catalog != nil {
		state.Consent = "approved"
		if err := b.service.validateCatalogMember(ctx, gate, member); err != nil {
			state.Consent = "unknown"
			if _, consent := errors.AsType[catalogConsentError](err); consent {
				state.Consent = "required"
			}
		} else {
			for _, outcome := range gate.catalog.MemberOutcomes {
				if outcome.Slug == member.slug {
					state.Authentication = "verified_at_consent"
					break
				}
			}
		}
	}
	if gate.catalogRecoveryOnly || (member.authClient != nil && member.authClient.outcome == authClientGap) {
		state.Consent = "required"
	}
	if state.Consent == "required" {
		state.Instructions = "Call tools.connect(server=...) and follow its URL and instructions to approve this server for the connection."
	}
	return state
}

func (b *metaCodeBackend) Connect(ctx context.Context, server string) (*codemode.ConnectionAction, error) {
	// Refresh authorization even when reusing a link from this execution.
	ctx, gate, members, err := b.connectionMembers(ctx)
	if err != nil {
		return nil, err
	}
	member, ok := findMetaMember(members, server)
	if !ok {
		return nil, codemode.ErrToolUnavailable
	}
	if !codeConnectAvailable(gate) {
		return nil, codemode.ErrToolUnavailable
	}
	needsConsent := b.knownConnectionState(ctx, gate, member).Consent == "required"
	b.connections.mu.Lock()
	defer b.connections.mu.Unlock()
	if previous := b.connections.actions[server]; previous != nil && previous.ExpiresAt != nil && time.Now().Before(*previous.ExpiresAt) {
		return previous, nil
	}
	endpoint := gate.authentication.endpoint
	root, err := url.Parse(gate.connectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve connection URL: %w", err)
	}
	origin := root.Scheme + "://" + root.Host
	ref, err := endpoint.EndpointRef(ctx, b.service.db, origin)
	if err != nil {
		return nil, fmt.Errorf("resolve connection endpoint: %w", err)
	}
	now := time.Now()
	expires := now.Add(10 * time.Minute)
	ticket := AuthnChallengeState{ //nolint:exhaustruct // The entry ticket carries only routing and identity binding; browser login supplies consent state.
		ID: uuid.NewString(), UserSessionIssuerID: gate.userSessionIssuerID,
		Endpoint: ref, CreatedAt: now, FirstParty: true,
		CodeReconnect: &CodeReconnectBinding{UserID: gate.authentication.subject.ID, Server: member.slug, ServerID: member.serverID, ExpiresAt: expires, Ticket: true, CatalogID: uuid.Nil, CredentialID: uuid.Nil, ClientID: "", CatalogComplete: false, ReviewReady: false},
	}
	if gate.catalog != nil && gate.catalogRef != nil && gate.catalogRef.Mode == toolfilter.CatalogModeCached &&
		member.backend == metaMemberBackendProxied {
		if err := b.bindReconnectCatalog(ctx, gate, member, &ticket); err != nil {
			b.service.logger.WarnContext(ctx, "catalogue reconnect unavailable; using full gateway reconnect", attr.SlogError(err))
			b.service.connections.Once(ctx, connectionScope(endpoint, mcpconnectionattempts.AppCodeMode), mcpconnectionattempts.StepDiscoveryMember, mcpconnectionattempts.CodeConsentNotApproved,
				mcpconnectionattempts.Member(member.serverID), mcpconnectionattempts.Subject(&gate.authentication.subject), mcpconnectionattempts.Detail("full_reconnect"))
			needsConsent = true
		}
	}
	if err := b.service.authnChallengeCache.Store(ctx, ticket); err != nil {
		return nil, fmt.Errorf("store connection request: %w", err)
	}
	link, err := url.Parse(gate.connectRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve connection URL: %w", err)
	}
	link = link.JoinPath("connect", "first-party")
	link.RawQuery = url.Values{"code_reconnect": {ticket.ID}}.Encode()
	instructions := "Open authorization_url and complete sign-in in your browser. Then tell the assistant to start a fresh execution and check access. No tool operation has been retried."
	reason := "upstream_sign_in"
	if ticket.CodeReconnect.CatalogID != uuid.Nil {
		if needsConsent {
			reason = "consent_required"
		}
		instructions = "Open authorization_url to connect this service, then review and approve the refreshed gateway tools. Then start a fresh execution and check access; do not replay an earlier program."
	} else if needsConsent {
		reason = "consent_required"
		instructions = "Open authorization_url to connect the upstream service. Then reconnect this gateway in your MCP client and approve this server. Browser sign-in alone does not renew the client's catalogue consent. Start a fresh execution after reconnecting; do not replay an earlier program."
	}
	action := &codemode.ConnectionAction{Server: server, Status: "authorization_required", Reason: reason, AuthorizationURL: link.String(), ExpiresAt: &expires, ClientReconnectRequired: needsConsent && ticket.CodeReconnect.CatalogID == uuid.Nil, Instructions: instructions}
	if b.connections.actions == nil {
		b.connections.actions = make(map[string]*codemode.ConnectionAction)
	}
	b.connections.actions[server] = action
	return action, nil
}

// codeConnectAvailable reports whether Connect can mint a link bound to the
// signed-in user for this gateway.
func codeConnectAvailable(gate *metaGateContext) bool {
	return gate.authentication != nil && gate.authentication.subject.Kind == urn.SessionSubjectKindUser &&
		gate.authentication.subject.ID != "" && gate.connectRoot != ""
}

// Per-execution reuse prevents concurrent helpers from issuing duplicate tickets.
type codeConnections struct {
	mu      sync.Mutex
	actions map[string]*codemode.ConnectionAction
}

// VerifyConnections refreshes authorization once for the page. It uses the
// consent page's live MCP probe without changing the frozen tool catalogue.
func (b *metaCodeBackend) VerifyConnections(ctx context.Context, servers []string) ([]*codemode.ConnectionState, error) {
	if len(servers) == 0 {
		return []*codemode.ConnectionState{}, nil
	}
	ctx, gate, members, err := b.currentState(ctx, true, true)
	if err != nil {
		return nil, err
	}
	states := make([]*codemode.ConnectionState, len(servers))
	var group errgroup.Group
	group.SetLimit(4)
	for i, server := range servers {
		group.Go(func() error {
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			states[i] = b.verifyConnection(probeCtx, gate, members, server)
			return nil
		})
	}
	_ = group.Wait() // Per-member failures are represented in states.
	return states, nil
}

func (b *metaCodeBackend) verifyConnection(ctx context.Context, gate *metaGateContext, members []metaMember, server string) *codemode.ConnectionState {
	member, ok := findMetaMember(members, server)
	if !ok {
		return &codemode.ConnectionState{Consent: "unknown", Authentication: "unknown", Instructions: "This server is no longer available; list servers again.", CheckedAt: nil}
	}
	state := b.knownConnectionState(ctx, gate, member)
	if ctx.Err() != nil {
		state.Authentication = "unknown"
		state.Instructions = "Verification time budget exhausted; verify this server separately."
		return state
	}
	if b.service.validationLimiter != nil {
		allowed, err := b.service.validationLimiter.Allow(ctx, "code:"+gate.userID+":"+member.serverID.String())
		switch {
		case err != nil:
			b.service.logger.WarnContext(ctx, "code connection validation limiter unavailable; allowing", attr.SlogError(err))
		case !allowed.Allowed:
			state.Authentication = "unknown"
			state.Instructions = "Connection verification is rate limited; try again later."
			return state
		}
	}
	// Resolve credentials afresh instead of treating consent-time verification as current.
	probeGate := *gate
	if gate.authentication != nil && gate.authentication.subject.Kind == urn.SessionSubjectKindUser {
		probeGate.chainUpstream = b.service.gatewayMemberChainer(gate, mcpidentity.KindUserSession, uuid.Nil)
	}
	dial, _, err := b.service.routeMetaMember(ctx, b.service.logger, probeGate, member, codeCallerIdentity(ctx, gate))
	now := time.Now().UTC()
	state.CheckedAt = &now
	if err != nil {
		_, needsSignIn := errors.AsType[memberCredentialError](err)
		if needsSignIn || (errors.Is(err, remotesessions.ErrNoValidToken) && !errors.Is(err, remotesessions.ErrRemoteSessionUnavailable) && !errors.Is(err, remotesessions.ErrRemoteSessionMisconfigured)) {
			state.Authentication = "reauth_required"
			state.Instructions = "Call tools.connect(server=...) to reconnect, then verify again in a fresh execution."
		} else {
			state.Authentication = "unknown"
			state.Instructions = "Connection verification could not complete; try again later."
		}
		return state
	}
	verdict, _ := b.service.probeUpstream(ctx, b.service.logger, probeProxyBuilder(dial.build), member.name)
	now = time.Now().UTC()
	state.CheckedAt = &now
	switch verdict {
	case remotesessions.ValidationOutcomeValid:
		state.Authentication = "connected"
	case remotesessions.ValidationOutcomeRejectedByMember, remotesessions.ValidationOutcomeInactive:
		state.Authentication = "reauth_required"
		state.Instructions = "Call tools.connect(server=...) to reconnect, then verify again in a fresh execution."
	default:
		state.Authentication = "unreachable"
		state.Instructions = "The upstream could not be verified; try again later."
	}
	return state
}

func (b *metaCodeBackend) bindReconnectCatalog(ctx context.Context, gate *metaGateContext, member metaMember, ticket *AuthnChallengeState) error {
	endpoint := gate.authentication.endpoint
	selector, err := b.service.upstreamSelectorFor(ctx, endpoint)
	if err != nil {
		return fmt.Errorf("bind reconnect to session: %w", err)
	}
	consent := *ticket
	consent.Subject = &gate.authentication.subject
	consent.FocusServer = member.slug
	clients, err := b.service.listEndpointClients(ctx, endpoint, consent)
	if err != nil {
		return fmt.Errorf("resolve reconnect clients: %w", err)
	}
	// Non-OAuth members still use full gateway consent: there is no provider
	// approval callback in which to update their catalogue.
	if selector != nil && len(subjectConnectedClients(clients)) > 0 {
		ticket.CodeReconnect.CatalogID = gate.catalog.ID
		ticket.CodeReconnect.ClientID = gate.catalog.ClientID
		ticket.CodeReconnect.CredentialID = selector.credentialID
	}
	return nil
}
