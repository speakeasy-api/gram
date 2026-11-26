package mcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type hostedAssignment struct {
	projectID      uuid.UUID
	registrationID uuid.UUID
	invalid        bool
}

func (s *Service) hostedAssignments(ctx context.Context, org string, serverIDs []uuid.UUID) (map[uuid.UUID]hostedAssignment, error) {
	if len(serverIDs) == 0 {
		return nil, nil
	}
	rows, err := upstreamclientsrepo.New(s.db).ListHostedUpstreamAssignments(ctx, upstreamclientsrepo.ListHostedUpstreamAssignmentsParams{OrganizationID: org, ServerIds: serverIDs})
	if err != nil {
		return nil, fmt.Errorf("load hosted upstream assignments: %w", err)
	}
	return indexHostedAssignments(rows), nil
}

func indexHostedAssignments(rows []upstreamclientsrepo.ListHostedUpstreamAssignmentsRow) map[uuid.UUID]hostedAssignment {
	assignments := make(map[uuid.UUID]hostedAssignment, len(rows))
	for _, row := range rows {
		_, duplicate := assignments[row.ServerID]
		assignments[row.ServerID] = hostedAssignment{
			projectID: row.ProjectID, registrationID: row.RegistrationID.UUID,
			invalid: duplicate || (!row.RegistrationID.Valid && hostedAssignmentRequired(row.RequiresUpstreamAuth, row.UserSessionIssuerID.Valid)),
		}
	}
	return assignments
}

func (a hostedAssignment) registration(projectID uuid.UUID) (uuid.UUID, error) {
	if projectID == uuid.Nil || a.projectID != projectID || a.invalid {
		return uuid.Nil, fmt.Errorf("%w: hosted server has no unambiguous upstream configuration", remotesessions.ErrRemoteSessionMisconfigured)
	}
	return a.registrationID, nil
}

// hostedRegistration reads the server assignment, never the issuer inventory.
func (s *Service) hostedRegistration(ctx context.Context, org string, projectID, serverID uuid.UUID) (uuid.UUID, error) {
	assignments, err := s.hostedAssignments(ctx, org, []uuid.UUID{serverID})
	if err != nil {
		return uuid.Nil, err
	}
	return assignments[serverID].registration(projectID)
}

// An explicit no-upstream decision is authoritative. A hosted server with no
// downstream issuer can also have no hosted sign-in configured. An undecided
// server with a sign-in must not silently degrade to no upstream assignment.
func hostedAssignmentRequired(required pgtype.Bool, hasSignIn bool) bool {
	if required.Valid {
		return required.Bool
	}
	return hasSignIn
}

func (s *Service) hostedSignInClients(ctx context.Context, org string, projectID, serverID uuid.UUID) ([]remotesessions.Client, error) {
	id, err := s.hostedRegistration(ctx, org, projectID, serverID)
	if err != nil || id == uuid.Nil {
		return nil, err
	}
	clients, err := s.remoteChallengeMgr.ListAuthClientRegistrations(ctx, projectID, org, []uuid.UUID{id})
	if err != nil {
		return nil, fmt.Errorf("load hosted upstream registration: %w", err)
	}
	if len(clients) != 1 {
		return nil, fmt.Errorf("%w: hosted upstream registration is unavailable", remotesessions.ErrRemoteSessionMisconfigured)
	}
	return clients, nil
}

func (s *Service) hostedUpstreamTokens(ctx context.Context, attribution remotesessions.RefreshAttribution, org string, projectID, serverID, issuerID uuid.UUID, subject urn.SessionSubject, skipUnusable bool) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	id, err := s.hostedRegistration(ctx, org, projectID, serverID)
	if err != nil || id == uuid.Nil {
		return nil, err
	}
	if issuerID == uuid.Nil {
		return nil, fmt.Errorf("%w: hosted upstream registration has no downstream sign-in", remotesessions.ErrRemoteSessionMisconfigured)
	}
	tokens, err := s.remoteChallengeMgr.ResolveHostedRegistrationAccessTokens(ctx, remotesessions.AuthClientTokenRequest{
		ProjectID: projectID, OrganizationID: org, UserSessionIssuerID: issuerID, Subject: subject, RegistrationID: id, SkipUnusable: skipUnusable, Attribution: attribution,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve exact hosted upstream registration: %w", err)
	}
	for issuer, token := range tokens {
		if token.RemoteSessionClientID != id || token.Resource != "" {
			delete(tokens, issuer)
		}
	}
	return tokens, nil
}

// hostedConsentRegistrations returns only reachable hosted placements for a
// consent subject. An issuer link never adds a registration to this set.
func (s *Service) hostedConsentRegistrations(ctx context.Context, endpoint *ResolvedMcpEndpoint, state AuthnChallengeState) (map[uuid.UUID]uuid.UUID, error) {
	if isDirectHostedValidation(endpoint) {
		id, err := s.hostedRegistration(ctx, endpoint.OrganizationID, endpoint.ProjectID, endpoint.McpServerID.UUID)
		if err != nil || id == uuid.Nil {
			return nil, err
		}
		return map[uuid.UUID]uuid.UUID{id: endpoint.ProjectID}, nil
	}
	if !endpoint.MetaMcpServerID.Valid && !endpoint.IsOrganizationGateway() {
		return nil, nil
	}
	members, err := s.consentSubjectMembers(ctx, endpoint, state)
	if err != nil {
		return nil, err
	}
	registrations := make(map[uuid.UUID]uuid.UUID)
	for _, member := range members {
		if member.backend != metaMemberBackendHosted {
			continue
		}
		for id := range member.associatedClients {
			registrations[id] = member.projectID
		}
	}
	return registrations, nil
}

var errHostedConsentSetupIncomplete = errors.New("hosted upstream authentication setup is incomplete")

const hostedSetupNotice = "An administrator must finish upstream authentication setup for this server."

// hostedConsentSetupIncomplete is the consent gate for incomplete hosted setup. A single
// reachable registration needs sign-in even when stored intent is NULL. This
// does not infer or persist intent for the runtime.
func (s *Service) hostedConsentSetupIncomplete(ctx context.Context, org string, projectID, serverID uuid.UUID) (bool, error) {
	_, err := s.hostedSignInClients(ctx, org, projectID, serverID)
	if errors.Is(err, remotesessions.ErrRemoteSessionMisconfigured) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read hosted consent setup: %w", err)
	}
	return false, nil
}

// markUnreachableHostedConsentPlacements checks exact project/registration
// pairs together. This is only for rendering; the serving and callback
// paths still do their own live authorization and resource checks.
func (s *Service) markUnreachableHostedConsentPlacements(ctx context.Context, organizationID string, members []metaMember) error {
	var projects, registrations []uuid.UUID
	for _, member := range members {
		if member.backend == metaMemberBackendHosted {
			for id := range member.associatedClients {
				projects = append(projects, member.projectID)
				registrations = append(registrations, id)
			}
		}
	}
	if len(registrations) == 0 {
		return nil
	}
	rows, err := s.db.Query(ctx, `
SELECT p.id, c.id
FROM unnest($2::uuid[], $3::uuid[]) AS placement(project_id, registration_id)
JOIN projects AS p ON p.id = placement.project_id AND p.organization_id = $1 AND NOT p.deleted
JOIN remote_session_clients AS c ON c.id = placement.registration_id AND NOT c.deleted
  AND (c.project_id = p.id OR (c.project_id IS NULL AND (c.organization_id IS NULL OR c.organization_id = p.organization_id)))
JOIN remote_session_issuers AS i ON i.id = c.remote_session_issuer_id AND NOT i.deleted
  AND (i.project_id = p.id OR (i.project_id IS NULL AND (i.organization_id IS NULL OR i.organization_id = p.organization_id)))`, organizationID, projects, registrations)
	if err != nil {
		return fmt.Errorf("list reachable hosted registrations: %w", err)
	}
	defer rows.Close()
	type placement struct{ project, registration uuid.UUID }
	live := make(map[placement]bool)
	for rows.Next() {
		var key placement
		if err := rows.Scan(&key.project, &key.registration); err != nil {
			return fmt.Errorf("scan reachable hosted registration: %w", err)
		}
		live[key] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read reachable hosted registrations: %w", err)
	}
	for i := range members {
		member := &members[i]
		if member.backend == metaMemberBackendHosted {
			for id := range member.associatedClients {
				if !live[placement{project: member.projectID, registration: id}] {
					member.associatedClients = nil
					member.upstreamUnavailable = errHostedConsentSetupIncomplete
				}
			}
		}
	}
	return nil
}
