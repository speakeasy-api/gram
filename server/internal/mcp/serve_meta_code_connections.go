package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
	state := &codemode.ConnectionState{Consent: "not_required", Authentication: "unknown", Instructions: ""}
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
		state.Instructions = "Call tools.connect(server=...) and show its URL and instructions. The user must also reconnect this gateway in their MCP client and approve the server; browser sign-in alone does not renew catalogue consent."
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
	if gate.authentication == nil || gate.authentication.subject.Kind != urn.SessionSubjectKindUser || gate.authentication.subject.ID == "" || gate.connectRoot == "" {
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
		CodeReconnect: &CodeReconnectBinding{UserID: gate.authentication.subject.ID, Server: member.slug, ServerID: member.serverID, ExpiresAt: expires, Ticket: true},
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
	if needsConsent {
		reason = "consent_required"
		instructions = "Open authorization_url to connect the upstream service. Then reconnect this gateway in your MCP client and approve this server. Browser sign-in alone does not renew the client's catalogue consent. Start a fresh execution after reconnecting; do not replay an earlier program."
	}
	action := &codemode.ConnectionAction{Server: server, Status: "authorization_required", Reason: reason, AuthorizationURL: link.String(), ExpiresAt: &expires, ClientReconnectRequired: needsConsent, Instructions: instructions}
	if b.connections.actions == nil {
		b.connections.actions = make(map[string]*codemode.ConnectionAction)
	}
	b.connections.actions[server] = action
	return action, nil
}

// Per-execution reuse prevents concurrent helpers from issuing duplicate tickets.
type codeConnections struct {
	mu      sync.Mutex
	actions map[string]*codemode.ConnectionAction
}
