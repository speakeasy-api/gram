// The consent page's upstream client pick per MCP server, which the minted
// credential records as its choice.

package mcp

import (
	"context"

	"github.com/google/uuid"
)

// consentUpstreamClientPicks returns, for each MCP server the consent covers,
// the upstream client the person is going through: the one card for it that
// is connected or chained, or, among several, the one preferred names. A
// server with no such card, or several and no preference among them, gets no
// pick, and the credential's choice for it is left to a reconnection unless
// the server has only one client. Interactive and chained cards are picked
// alike, so this is the one place a person's choice is decided.
func (s *Service) consentUpstreamClientPicks(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, cards []remoteSessionCard, preferred map[uuid.UUID]uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	var members []metaMember
	var err error
	if endpoint.MetaMcpServerID.Valid {
		members, err = s.consentSubjectMembers(ctx, endpoint, challengeState)
		if err != nil {
			return nil, err
		}
	}
	return chooseConsentUpstreamClientPicks(endpoint, members, cards, preferred), nil
}

// usableConsentCard is a card a pick can go through. An outage leaves a connected card's connection in place, as personalReadiness treats it.
func usableConsentCard(card remoteSessionCard) bool {
	switch cardStatus(card) {
	case gatewayServerConnected, gatewayServerChained, gatewayServerChainPending, gatewayServerUnavailable:
		return true
	case gatewayServerNoSignIn, gatewayServerNotSetUp, gatewayServerNotConnected, gatewayServerReconnect, gatewayServerNoAccess:
	}
	return false
}

func chooseConsentUpstreamClientPicks(endpoint *ResolvedMcpEndpoint, members []metaMember, cards []remoteSessionCard, preferred map[uuid.UUID]uuid.UUID) map[uuid.UUID]uuid.UUID {
	var usable []remoteSessionCard
	for _, card := range cards {
		if usableConsentCard(card) {
			usable = append(usable, card)
		}
	}
	picks := map[uuid.UUID]uuid.UUID{}
	pick := func(serverID uuid.UUID, candidates []remoteSessionCard) {
		want, hasPreference := preferred[serverID]
		var healthy []uuid.UUID
		for _, card := range candidates {
			clientID, err := uuid.Parse(card.ClientID)
			if err != nil {
				continue
			}
			if len(candidates) == 1 || (hasPreference && clientID == want) {
				picks[serverID] = clientID
				return
			}
			if cardStatus(card) != gatewayServerUnavailable {
				healthy = append(healthy, clientID)
			}
		}
		// With no preference to follow, the one service not in an outage is the default.
		if len(healthy) == 1 {
			picks[serverID] = healthy[0]
		}
	}
	switch {
	case endpoint.MetaMcpServerID.Valid:
		for _, member := range members {
			var candidates []remoteSessionCard
			for _, card := range usable {
				if cardServesMember(card, member) {
					candidates = append(candidates, card)
				}
			}
			pick(member.serverID, candidates)
		}
	case endpoint.McpServerID.Valid:
		pick(endpoint.McpServerID.UUID, usable)
	}
	return picks
}

// inventoryUpstreamPicks is the service each member's captured tools came through.
func inventoryUpstreamPicks(inventory *consentToolInventory) map[uuid.UUID]uuid.UUID {
	if inventory == nil {
		return nil
	}
	picks := make(map[uuid.UUID]uuid.UUID, len(inventory.MemberBindings))
	for _, member := range inventory.MemberBindings {
		if member.Auth != nil && member.Auth.RegistrationID.Valid {
			picks[member.ServerID] = member.Auth.RegistrationID.UUID
		}
	}
	return picks
}
