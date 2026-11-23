package mcp

import (
	"errors"
	"slices"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

var errConsentPreviewGrantMissing = errors.New("chosen consent credential requires sign-in")

// consentDiscoveryFlowID is correlation metadata, not the state that
// authorizes consent. Never surface an opaque or arbitrary string instead.
func consentDiscoveryFlowID(raw string) string {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return ""
	}
	return id.String()
}

func consentReconnectTargets(auth *consentServerAuth, cards []remoteSessionCard) (string, []string) {
	const prefix = "consent-reconnect-"
	if auth == nil {
		// A legacy preview does not post a credential. Do not guess which
		// registration to reconnect when there are several service cards.
		if len(cards) == 1 {
			return prefix + cards[0].ClientID, []string{cards[0].ClientID}
		}
		return "", nil
	}
	var clients []string
	for i := range auth.Options {
		option := &auth.Options[i]
		card := consentPersonalCredentialCard(*option, cards)
		if card == nil {
			continue
		}
		option.ReconnectFormID = prefix + card.ClientID
		if !slices.Contains(clients, card.ClientID) {
			clients = append(clients, card.ClientID)
		}
	}
	if selected := auth.Selected(); selected != nil {
		return selected.ReconnectFormID, clients
	}
	return "", clients
}

func consentPersonalCredentialCard(option consentAuthClientOption, cards []remoteSessionCard) *remoteSessionCard {
	if !option.registrationID.Valid || (option.Type != authz.AuthClientTypeRemoteSession && option.Type != authz.AuthClientTypeEMA) {
		return nil
	}
	registration := option.registrationID.UUID.String()
	for i := range cards {
		if cards[i].ClientID == registration {
			return &cards[i]
		}
	}
	return nil
}

func consentPreviewGrantMissing(auth *consentServerAuth, picked uuid.UUID, cards []remoteSessionCard) bool {
	if picked == uuid.Nil {
		return false
	}
	for _, option := range auth.Options {
		if option.ID != picked || option.State == consentClientSetup {
			continue
		}
		card := consentPersonalCredentialCard(option, cards)
		// Do not turn a routing or dependency failure, or an untested
		// identity chain, into an OAuth redirect.
		return card != nil && !card.connectionDown() && !card.Unroutable && !card.Chained &&
			(card.Expired || card.Rejected || card.Inactive || !card.Connected)
	}
	return false
}
