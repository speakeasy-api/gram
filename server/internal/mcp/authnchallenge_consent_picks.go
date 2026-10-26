// The consent page's upstream client pick per MCP server, which the minted
// credential records as its choice.

package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// consentUpstreamClientPicks returns, for each MCP server the consent covers,
// the upstream client the person is going through: the one card for it that
// is connected or chained. A server with no such card, or several, gets no
// pick, and the credential's choice for it is left to a reconnection unless
// the server has only one client. Interactive and chained cards are picked
// alike, so this is the one place a person's choice is decided.
func (s *Service) consentUpstreamClientPicks(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState) (map[uuid.UUID]uuid.UUID, error) {
	cards, err := s.buildRemoteSessionCards(ctx, endpoint, challengeState, s.resolveAutoRefreshPolicy(ctx, endpoint.OrganizationID))
	if err != nil {
		return nil, fmt.Errorf("build remote session cards: %w", err)
	}
	var members []metaMember
	if endpoint.MetaMcpServerID.Valid {
		members, err = s.consentSubjectMembers(ctx, endpoint, challengeState)
		if err != nil {
			return nil, err
		}
	}
	return chooseConsentUpstreamClientPicks(endpoint, members, cards), nil
}

func chooseConsentUpstreamClientPicks(endpoint *ResolvedMcpEndpoint, members []metaMember, cards []remoteSessionCard) map[uuid.UUID]uuid.UUID {
	var usable []remoteSessionCard
	for _, card := range cards {
		switch cardStatus(card) {
		case gatewayServerConnected, gatewayServerChained, gatewayServerChainPending:
			usable = append(usable, card)
		case gatewayServerNoSignIn, gatewayServerNotSetUp, gatewayServerNotConnected, gatewayServerReconnect, gatewayServerUnavailable, gatewayServerNoAccess:
		}
	}
	picks := map[uuid.UUID]uuid.UUID{}
	pick := func(serverID uuid.UUID, candidates []remoteSessionCard) {
		if len(candidates) != 1 {
			return
		}
		if clientID, err := uuid.Parse(candidates[0].ClientID); err == nil {
			picks[serverID] = clientID
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
