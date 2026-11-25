package mcp

import (
	"context"
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
	duplicate      bool
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
			invalid:   !row.RegistrationID.Valid && hostedAssignmentRequired(row.RequiresUpstreamAuth, row.UserSessionIssuerID.Valid),
			duplicate: duplicate,
		}
	}
	return assignments
}

func (a hostedAssignment) registration(projectID uuid.UUID) (uuid.UUID, error) {
	if projectID == uuid.Nil || a.projectID != projectID || a.invalid || a.duplicate {
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
	assignment := assignments[serverID]
	if assignment.duplicate && assignment.projectID == projectID {
		// An association can outlive a soft-deleted registration. The
		// existing live-association read disambiguates that case; keep
		// zero or multiple live registrations fail-closed.
		rows, err := upstreamclientsrepo.New(s.db).ListMCPServerRemoteSessionClients(ctx, upstreamclientsrepo.ListMCPServerRemoteSessionClientsParams{
			McpServerID: serverID,
			ProjectID:   projectID,
		})
		if err != nil {
			return uuid.Nil, fmt.Errorf("check live hosted upstream assignments: %w", err)
		}
		if len(rows) == 1 {
			assignment.duplicate = false
			assignment.registrationID = rows[0].RemoteSessionClientID
		}
	}
	return assignment.registration(projectID)
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
	return exactHostedMemberTokens(tokens, id)
}

// A required credential is not permission to dispatch anonymously.
func exactHostedMemberTokens(tokens map[uuid.UUID]remotesessions.UpstreamToken, registrationID uuid.UUID) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	for _, token := range tokens {
		if token.RemoteSessionClientID != registrationID || token.Resource != "" {
			continue
		}
		if token.ClientCredentialErr != nil {
			return nil, token.ClientCredentialErr
		}
		if token.Token != "" && len(tokens) == 1 {
			return tokens, nil
		}
	}
	return nil, remotesessions.ErrNoValidToken
}
