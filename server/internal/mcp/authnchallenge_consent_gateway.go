// Gateway consent: a gateway signs a person in to every server it fronts, so
// its consent page approves the gateway alone and lists the servers behind a
// disclosure. Each server has its own connect page, reached from that list or
// from a tool call that needed the person's sign-in, where they sign in to the
// services that one server uses. Connecting is all a server needs: there is
// nothing to approve on its page.

package mcp

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"

	"github.com/google/uuid"
)

// gatewayListOpenLimit is the most servers a gateway's page lists open by
// default; a longer list starts collapsed behind its summary.
const gatewayListOpenLimit = 5

// maxFocusServerLength bounds a gateway server named in a connect URL or
// card form. Server slugs, qualified by their project on an organization
// gateway, are far shorter.
const maxFocusServerLength = 256

// gatewayServerStatus is one server's readiness on the gateway consent page.
type gatewayServerStatus string

const (
	// gatewayServerNoSignIn is a server whose upstream needs no per-person
	// sign-in, so it works as soon as the gateway is authorized.
	gatewayServerNoSignIn gatewayServerStatus = "no_sign_in"
	// gatewayServerConnected is a server with a usable connection.
	gatewayServerConnected gatewayServerStatus = "connected"
	// gatewayServerChained is a server the person's identity provider signs
	// them in to, without a sign-in of their own, once that exchange is
	// known to work.
	gatewayServerChained gatewayServerStatus = "chained"
	// gatewayServerChainPending is a server the identity provider is set up
	// to sign the person in to, before the exchange is known to work.
	gatewayServerChainPending gatewayServerStatus = "chain_pending"
	// gatewayServerReconnect is a server whose connection must be redone.
	gatewayServerReconnect gatewayServerStatus = "reconnect"
	// gatewayServerNotConnected is a server the person has not connected.
	gatewayServerNotConnected gatewayServerStatus = "not_connected"
	// gatewayServerNotSetUp is a server whose upstream signs people in, but
	// through no service linked to this gateway's sign-in, so nobody can
	// connect it until an administrator links one.
	gatewayServerNotSetUp gatewayServerStatus = "not_set_up"
)

// gatewayServer is one server listed on a gateway's consent page.
type gatewayServer struct {
	// Slug addresses the server's connect page and is what tool calls name.
	Slug string
	// Name is the server's display name, falling back to its slug.
	Name   string
	Status gatewayServerStatus
	// ConnectClientID is the one service the gateway's list connects in
	// place, set when the server signs in through exactly one service and
	// needs connecting. A server with several services connects on its own
	// page instead, where each is named.
	ConnectClientID string
	// cardIDs are the services the server signs in through.
	cardIDs []string
}

// Ready reports whether the server works without any further action.
func (g gatewayServer) Ready() bool {
	switch g.Status {
	case gatewayServerNoSignIn, gatewayServerConnected, gatewayServerChained:
		return true
	case gatewayServerChainPending, gatewayServerReconnect, gatewayServerNotConnected, gatewayServerNotSetUp:
		return false
	}
	return false
}

// StatusLabel is the server's status line.
func (g gatewayServer) StatusLabel() string {
	switch g.Status {
	case gatewayServerNoSignIn:
		return "Ready · no sign-in needed"
	case gatewayServerConnected:
		return "Connected"
	case gatewayServerChained:
		return "Connected through your identity provider"
	case gatewayServerChainPending:
		return "Managed by your identity provider"
	case gatewayServerReconnect:
		return "Needs reconnecting"
	case gatewayServerNotConnected:
		return "Not connected"
	case gatewayServerNotSetUp:
		return "Sign-in not set up"
	}
	return ""
}

// NeedsAttention reports whether the status line should warn.
func (g gatewayServer) NeedsAttention() bool {
	return g.Status == gatewayServerReconnect || g.Status == gatewayServerNotSetUp
}

// NeedsSignIn reports whether the server signs in through a service the
// person can connect.
func (g gatewayServer) NeedsSignIn() bool {
	return g.Status != gatewayServerNoSignIn && g.Status != gatewayServerNotSetUp
}

// NotSetUp reports whether no service the person can connect signs in to
// the server.
func (g gatewayServer) NotSetUp() bool {
	return g.Status == gatewayServerNotSetUp
}

// gatewayConsentView is what a gateway's consent page renders in place of
// the flat list of services.
type gatewayConsentView struct {
	// Servers are the servers the person can reach through the gateway,
	// those needing action first.
	Servers    []gatewayServer
	ReadyCount int

	// ListOpen lists the servers open by default: a short list, or a
	// first-party page, which has nothing else on it.
	ListOpen bool

	// Focus is the server whose connect page is rendered, nil on the
	// gateway's own page.
	Focus *gatewayServer

	// FocusUnavailable marks a connect page requested for a server the
	// person cannot reach, which renders the gateway's own page instead.
	FocusUnavailable bool
}

// gatewayConsent builds the gateway consent view for the challenge's
// subject. focus names the server whose connect page is requested, empty for
// the gateway's own page. It returns the cards the page renders: those of the
// focused server, or none on the gateway's own page, which lists servers
// instead.
func (s *Service) gatewayConsent(
	ctx context.Context,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	cards []remoteSessionCard,
	focus string,
) (*gatewayConsentView, []remoteSessionCard, error) {
	members, err := s.consentSubjectMembers(ctx, endpoint, challengeState)
	if err != nil {
		return nil, nil, err
	}
	view := gatewayServersView(members, cards)
	view.ListOpen = challengeState.FirstParty || len(view.Servers) <= gatewayListOpenLimit
	if focus == "" {
		return view, nil, nil
	}
	for i := range view.Servers {
		if view.Servers[i].Slug != focus {
			continue
		}
		focused := view.Servers[i]
		view.Focus = &focused
		return view, focusedCards(view, focused, cards), nil
	}
	view.FocusUnavailable = true
	return view, nil, nil
}

// gatewayServersView lists members with the status their cards give them.
// A card serves a member when it signs in to the member's authorization
// server, from the member's own project unless no project owns it, the same
// match clientServesAnyMember makes.
func gatewayServersView(members []metaMember, cards []remoteSessionCard) *gatewayConsentView {
	view := &gatewayConsentView{Servers: make([]gatewayServer, 0, len(members)), ReadyCount: 0, ListOpen: false, Focus: nil, FocusUnavailable: false}
	for _, member := range members {
		server := gatewayServer{Slug: member.slug, Name: cmp.Or(member.name, member.slug), Status: gatewayServerNoSignIn, ConnectClientID: "", cardIDs: nil}
		for _, card := range cards {
			if cardServesMember(card, member) {
				server.cardIDs = append(server.cardIDs, card.ClientID)
				server.Status = strongerStatus(server.Status, cardStatus(card))
			}
		}
		if memberNeedsSignIn(member) && len(server.cardIDs) == 0 {
			server.Status = gatewayServerNotSetUp
		}
		if len(server.cardIDs) == 1 && (server.Status == gatewayServerNotConnected || server.Status == gatewayServerReconnect) {
			server.ConnectClientID = server.cardIDs[0]
		}
		if server.Ready() {
			view.ReadyCount++
		}
		view.Servers = append(view.Servers, server)
	}
	slices.SortStableFunc(view.Servers, func(a, b gatewayServer) int {
		return cmp.Or(cmp.Compare(statusOrder(a.Status), statusOrder(b.Status)), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Slug, b.Slug))
	})
	return view
}

func cardServesMember(card remoteSessionCard, member metaMember) bool {
	clientID := uuid.Nil
	if member.associatedClients != nil {
		parsed, err := uuid.Parse(card.ClientID)
		if err != nil {
			return false
		}
		clientID = parsed
	}
	return memberUsesClient(member, clientID, card.remoteSessionIssuerID, card.projectID)
}

// cardStatus is the status one service gives the servers it signs in to.
func cardStatus(card remoteSessionCard) gatewayServerStatus {
	reconnect := card.Expired || card.Unroutable || card.IdentityReconnect || card.Rejected || card.Inactive
	switch {
	case card.Connected && !reconnect:
		return gatewayServerConnected
	case card.Chained && card.ChainCheck == chainCheckConnected:
		return gatewayServerChained
	case card.Chained && card.ChainCheck != chainCheckRejected:
		return gatewayServerChainPending
	case reconnect:
		return gatewayServerReconnect
	default:
		return gatewayServerNotConnected
	}
}

// strongerStatus combines a server's services: one usable service makes the
// server usable, since its calls route to whichever holds a credential.
func strongerStatus(current, next gatewayServerStatus) gatewayServerStatus {
	if current == gatewayServerNoSignIn {
		return next
	}
	if readiness(next) > readiness(current) {
		return next
	}
	return current
}

// readiness ranks how close a service brings its server to working, with no
// ties, so a server's status does not depend on the order of its services. A
// pending identity-provider sign-in outranks a lapsed connection: with no
// usable token the gateway signs in through the identity provider.
func readiness(status gatewayServerStatus) int {
	switch status {
	case gatewayServerConnected:
		return 5
	case gatewayServerChained:
		return 4
	case gatewayServerChainPending:
		return 3
	case gatewayServerReconnect:
		return 2
	case gatewayServerNotConnected:
		return 1
	case gatewayServerNoSignIn, gatewayServerNotSetUp:
		return 0
	}
	return 0
}

// statusOrder sorts servers needing action before servers that work.
func statusOrder(status gatewayServerStatus) int {
	switch status {
	case gatewayServerReconnect:
		return 0
	case gatewayServerNotConnected:
		return 1
	case gatewayServerNotSetUp, gatewayServerChainPending:
		return 2
	case gatewayServerChained, gatewayServerConnected:
		return 3
	case gatewayServerNoSignIn:
		return 4
	}
	return 5
}

// focusedCards returns the focused server's cards, each noting how many other
// servers on the gateway it also signs in to.
func focusedCards(view *gatewayConsentView, focused gatewayServer, cards []remoteSessionCard) []remoteSessionCard {
	out := make([]remoteSessionCard, 0, len(focused.cardIDs))
	for _, card := range cards {
		if !slices.Contains(focused.cardIDs, card.ClientID) {
			continue
		}
		others := 0
		for _, server := range view.Servers {
			if server.Slug != focused.Slug && slices.Contains(server.cardIDs, card.ClientID) {
				others++
			}
		}
		if others > 0 {
			card.SharedServerNote = fmt.Sprintf("Also connects %d other %s on this gateway", others, pluralize(others, "server"))
		}
		out = append(out, card)
	}
	return out
}

// gatewayServerConnectURL is the URL that signs a person in and opens one
// gateway server's connect page, or empty when the gateway cannot serve it.
func gatewayServerConnectURL(endpointRoot string, serverSlug string) string {
	if endpointRoot == "" || serverSlug == "" {
		return ""
	}
	u, err := url.Parse(endpointRoot)
	if err != nil {
		return ""
	}
	u = u.JoinPath("connect", "first-party")
	u.RawQuery = url.Values{"server": []string{serverSlug}}.Encode()
	return u.String()
}

// memberConnectURL is the URL a person opens to connect member on this
// gateway, or empty when the gateway has no connect pages or the member signs
// in through no service.
func (g metaGateContext) memberConnectURL(member metaMember) string {
	if g.connectRoot == "" || !member.remoteSessionIssuerID.Valid {
		return ""
	}
	return gatewayServerConnectURL(g.connectRoot, member.slug)
}
