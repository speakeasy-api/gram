package mcp

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/codemode"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var _ codemode.ConnectionBackend = (*metaCodeBackend)(nil)

// Recovery uses the current visible member set, including members not yet
// approved at consent, but never turns that visibility into tool authority.
func (b *metaCodeBackend) connectionMembers(ctx context.Context) (context.Context, *metaGateContext, []metaMember, error) {
	return b.currentState(ctx, false, true)
}

func (b *metaCodeBackend) ConnectionStatus(ctx context.Context, server string) (*codemode.ConnectionStatus, error) {
	ctx, gate, members, err := b.connectionMembers(ctx)
	if err != nil {
		return nil, err
	}
	result := &codemode.ConnectionStatus{Servers: []codemode.ConnectionState{}}
	found := server == ""
	slices.SortFunc(members, func(a, b metaMember) int { return cmp.Compare(a.slug, b.slug) })
	for _, member := range members {
		if server != "" && server != member.slug {
			continue
		}
		found = true
		status, detail := b.connectionSignInStatus(ctx, gate, member)
		if gate.catalogRecoveryOnly {
			status, detail = metamcp.SignInNeedsConsent, "The approved catalogue expired. Reconnect this gateway in your MCP client and approve this server."
		}
		result.Servers = append(result.Servers, codemode.ConnectionState{Server: member.slug, Name: member.name, Status: status, Reason: detail, Instructions: connectionInstructions(status)})
	}
	if !found {
		return nil, codemode.ErrToolUnavailable
	}
	return result, nil
}

func connectionInstructions(status string) string {
	switch status {
	case metamcp.SignInNeedsConsent:
		return "Call tools.connect(server=...) for the browser sign-in link. Then reconnect this gateway in your MCP client and approve the server; browser sign-in alone does not renew catalogue consent."
	case metamcp.SignInNotConnected:
		return "Call tools.connect(server=...) and show its authorization_url and instructions to the user."
	case metamcp.SignInVerifiedAtConsent:
		return "This records consent-time verification, not current upstream access. If a call requires sign-in, use tools.connect(server=...)."
	default:
		return "If the upstream requests a new sign-in, call tools.connect(server=...). Start a fresh execution after the user finishes."
	}
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
	status, _ := b.connectionSignInStatus(ctx, gate, member)
	needsConsent := gate.catalogRecoveryOnly || status == metamcp.SignInNeedsConsent
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

func (b *metaCodeBackend) connectionSignInStatus(ctx context.Context, gate *metaGateContext, member metaMember) (string, string) {
	if member.authClient != nil && member.authClient.outcome == authClientGap {
		return metamcp.SignInNeedsConsent, "Reconnect this gateway in your MCP client to choose its upstream connection."
	}
	return b.service.memberSignInStatus(ctx, b.service.logger, gate, member)
}
