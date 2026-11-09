package mcp

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// typedOfferedRegistrations is, where typed auth client resolution is
// enforced, the OAuth registrations each of serverIDs offers through a
// personal or identity-provider auth client. Such a registration need not be
// linked to the sign-in or associated with the server the older way, so
// consent adds it to the services a person can connect. It is nil elsewhere,
// where consent keeps the sign-in's clients alone.
func (s *Service) typedOfferedRegistrations(ctx context.Context, organizationID string, serverIDs []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	if len(serverIDs) == 0 {
		return nil, nil
	}
	enforced, err := s.consentAuthChoicesEnforced(ctx, organizationID)
	if err != nil || !enforced {
		return nil, err
	}
	rows, err := upstreamclientsrepo.New(s.db).ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: organizationID, McpServerIds: serverIDs})
	if err != nil {
		return nil, fmt.Errorf("list typed auth client registrations: %w", err)
	}
	offered := map[uuid.UUID][]uuid.UUID{}
	for _, row := range rows {
		if !row.RemoteSessionClientID.Valid {
			continue
		}
		if row.ClientType.String != authz.AuthClientTypeRemoteSession && row.ClientType.String != authz.AuthClientTypeEMA {
			continue
		}
		offered[row.McpServerID] = append(offered[row.McpServerID], row.RemoteSessionClientID.UUID)
	}
	return offered, nil
}

// withTypedMemberRegistrations makes the registrations each proxied member
// offers through typed auth clients its associated clients, where typed auth
// client resolution is enforced, so consent matches a service to the members
// the resolver serves through it. A member offering none is matched to none.
// Elsewhere members keep the clients associated with them.
func (s *Service) withTypedMemberRegistrations(ctx context.Context, organizationID string, members []metaMember) error {
	ids := make([]uuid.UUID, 0, len(members))
	for _, member := range members {
		if member.backend == metaMemberBackendProxied {
			ids = append(ids, member.serverID)
		}
	}
	offered, err := s.typedOfferedRegistrations(ctx, organizationID, ids)
	if err != nil || offered == nil {
		return err
	}
	for i := range members {
		if members[i].backend != metaMemberBackendProxied {
			continue
		}
		registrations := offered[members[i].serverID]
		clients := make(map[uuid.UUID]bool, len(registrations))
		for _, id := range registrations {
			clients[id] = true
		}
		members[i].associatedClients = clients
		members[i].typedRegistrations = true
	}
	return nil
}

// typedConsentRegistrations maps each registration the endpoint's servers
// offer through typed auth clients to the project it is served in: the
// endpoint's own for a direct server, each member's for a gateway. It is
// empty where typed auth client resolution is not enforced.
func (s *Service) typedConsentRegistrations(ctx context.Context, endpoint *ResolvedMcpEndpoint, loadMembers func() ([]metaMember, error)) (map[uuid.UUID]uuid.UUID, error) {
	if (!endpoint.McpServerID.Valid || endpoint.ToolsetID.Valid) && !endpoint.MetaMcpServerID.Valid && !endpoint.IsOrganizationGateway() {
		return nil, nil
	}
	enforced, err := s.consentAuthChoicesEnforced(ctx, endpoint.OrganizationID)
	if err != nil || !enforced {
		return nil, err
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
	return s.withTypedRegistrationStatusesForResource(ctx, organizationID, subject, statuses, typed, "")
}

func (s *Service) withTypedRegistrationStatusesForResource(ctx context.Context, organizationID string, subject urn.SessionSubject, statuses map[uuid.UUID]remotesessions.RemoteSessionState, typed map[uuid.UUID]uuid.UUID, resource string) (map[uuid.UUID]remotesessions.RemoteSessionState, error) {
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
		found, err := s.remoteChallengeMgr.AuthClientRemoteSessionStatusesForResource(ctx, subject, projectID, organizationID, ids, resource)
		if err != nil {
			return nil, fmt.Errorf("typed auth client session statuses: %w", err)
		}
		maps.Copy(merged, found)
	}
	return merged, nil
}
