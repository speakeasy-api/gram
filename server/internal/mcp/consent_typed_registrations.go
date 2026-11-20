package mcp

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// typedOfferedRegistrations returns the OAuth registrations each server offers
// through personal or identity-provider auth clients, independently of sign-in links.
func (s *Service) typedOfferedRegistrations(ctx context.Context, organizationID string, serverIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	if len(serverIDs) == 0 {
		return nil, nil
	}
	offers, err := s.configuredAuthOffers(ctx, organizationID, serverIDs)
	if err != nil {
		return nil, err
	}
	offered := map[uuid.UUID][]uuid.UUID{}
	for id, offer := range offers {
		if registrations := offer.personalRegistrations(); len(registrations) > 0 {
			offered[id] = registrations
		}
	}
	return offered, nil
}

// withTypedMemberRegistrations makes the registrations each proxied member
// offers through typed auth clients its associated clients. A proxied member
// offering none is matched to none; hosted members keep their own sign-in.
func (s *Service) withTypedMemberRegistrations(ctx context.Context, organizationID string, members []metaMember) error {
	ids := make([]uuid.UUID, 0, len(members))
	for _, member := range members {
		if member.backend == metaMemberBackendProxied {
			ids = append(ids, member.serverID)
		}
	}
	offers, err := s.configuredAuthOffers(ctx, organizationID, ids)
	if err != nil {
		return err
	}
	applyTypedMemberRegistrations(members, offers)
	return nil
}

func applyTypedMemberRegistrations(members []metaMember, offers map[uuid.UUID]configuredAuthOffer) {
	for i := range members {
		if members[i].backend != metaMemberBackendProxied {
			continue
		}
		registrations := offers[members[i].serverID].personalRegistrations()
		clients := make(map[uuid.UUID]bool, len(registrations))
		for _, id := range registrations {
			clients[id] = true
		}
		members[i].associatedClients = clients
		members[i].typedRegistrations = true
	}
}

// recheckLegacyInventoryKey keeps AIM-1149's existing unenforced keepalive
// behavior separate from browser consent. No browser path sets this key.
type recheckLegacyInventoryKey struct{}

func usesTypedConsentInventory(ctx context.Context) bool {
	legacy, _ := ctx.Value(recheckLegacyInventoryKey{}).(bool)
	return !legacy
}

func directProxiedConsent(endpoint *ResolvedMcpEndpoint) bool {
	return endpoint.McpServerID.Valid && !endpoint.ToolsetID.Valid && !endpoint.MetaMcpServerID.Valid && !endpoint.IsOrganizationGateway()
}

// typedConsentRegistrations maps each registration the endpoint offers to
// the project that serves it. Hosted endpoints have no typed registrations.
func (s *Service) typedConsentRegistrations(ctx context.Context, endpoint *ResolvedMcpEndpoint, loadMembers func() ([]metaMember, error)) (map[uuid.UUID]uuid.UUID, error) {
	if !usesTypedConsentInventory(ctx) {
		return nil, nil
	}
	if (!endpoint.McpServerID.Valid || endpoint.ToolsetID.Valid) && !endpoint.MetaMcpServerID.Valid && !endpoint.IsOrganizationGateway() {
		return nil, nil
	}
	projects := map[uuid.UUID]uuid.UUID{}
	switch {
	case endpoint.McpServerID.Valid && !endpoint.ToolsetID.Valid:
		offered, err := s.typedOfferedRegistrations(ctx, endpoint.OrganizationID, []uuid.UUID{endpoint.McpServerID.UUID})
		if err != nil {
			return nil, err
		}
		for _, id := range offered[endpoint.McpServerID.UUID] {
			projects[id] = endpoint.ProjectID
		}
	case endpoint.MetaMcpServerID.Valid || endpoint.IsOrganizationGateway():
		members, err := loadMembers()
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			if !member.typedRegistrations {
				continue
			}
			for id := range member.associatedClients {
				if _, ok := projects[id]; !ok {
					projects[id] = member.projectID
				}
			}
		}
	}
	return projects, nil
}

// withTypedRegistrationClients adds the registrations in typed that clients
// lacks, loaded where the auth client resolver serves them. A registration out
// of the project's reach is left out.
func (s *Service) withTypedRegistrationClients(ctx context.Context, organizationID string, clients []remotesessions.Client, typed map[uuid.UUID]uuid.UUID) ([]remotesessions.Client, error) {
	byProject := map[uuid.UUID][]uuid.UUID{}
	for id, projectID := range typed {
		if slices.ContainsFunc(clients, func(c remotesessions.Client) bool { return c.ID == id }) {
			continue
		}
		byProject[projectID] = append(byProject[projectID], id)
	}
	for projectID, ids := range byProject {
		loaded, err := s.remoteChallengeMgr.ListAuthClientRegistrations(ctx, projectID, organizationID, ids)
		if err != nil {
			return nil, fmt.Errorf("list typed auth client registrations: %w", err)
		}
		clients = append(clients, loaded...)
	}
	return clients, nil
}

// withTypedRegistrationStatuses reads the subject's session state for the
// registrations in typed where the auth client resolver would serve them, in
// place of what statuses holds for them.
func (s *Service) withTypedRegistrationStatuses(ctx context.Context, organizationID string, subject urn.SessionSubject, statuses map[uuid.UUID]remotesessions.RemoteSessionState, typed map[uuid.UUID]uuid.UUID) (map[uuid.UUID]remotesessions.RemoteSessionState, error) {
	if len(typed) == 0 {
		return statuses, nil
	}
	byProject := map[uuid.UUID][]uuid.UUID{}
	for id, projectID := range typed {
		byProject[projectID] = append(byProject[projectID], id)
	}
	merged := make(map[uuid.UUID]remotesessions.RemoteSessionState, len(statuses))
	maps.Copy(merged, statuses)
	maps.DeleteFunc(merged, func(id uuid.UUID, _ remotesessions.RemoteSessionState) bool {
		_, ok := typed[id]
		return ok
	})
	for projectID, ids := range byProject {
		found, err := s.remoteChallengeMgr.AuthClientRemoteSessionStatuses(ctx, subject, projectID, organizationID, ids)
		if err != nil {
			return nil, fmt.Errorf("typed auth client session statuses: %w", err)
		}
		maps.Copy(merged, found)
	}
	return merged, nil
}
