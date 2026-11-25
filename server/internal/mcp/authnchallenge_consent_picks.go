package mcp

import "github.com/google/uuid"

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
