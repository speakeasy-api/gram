package remotesessions

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// AuthClientConnectionsElsewhere reports existence only, never a representative
// grant. It preserves the typed registration's tenant/subject selectors without
// requiring a legacy sign-in link. Callers must still authorize the auth client.
func (m *ChallengeManager) AuthClientConnectionsElsewhere(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, clientIDs []uuid.UUID, resource string) (map[uuid.UUID]bool, error) {
	if subject.IsZero() || resource == "" || len(clientIDs) == 0 {
		return nil, nil
	}
	rows, err := remotesessionsrepo.New(m.db).ListRemoteSessionStatusesInTenant(ctx, remotesessionsrepo.ListRemoteSessionStatusesInTenantParams{
		ProjectID: projectID, OrganizationID: organizationID, SubjectUrn: subject, RemoteSessionClientIds: clientIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("list typed connections elsewhere: %w", err)
	}
	states := make([]remotesessionsrepo.ListRemoteSessionStatusesForSubjectRow, 0, len(rows))
	for _, row := range rows {
		states = append(states, remotesessionsrepo.ListRemoteSessionStatusesForSubjectRow(row))
	}
	return connectionsElsewhere(states, resource), nil
}

// RemoteSessionConnectionsElsewhere retains the compatibility reader's legacy
// sign-in link and tenant/subject selectors. Only existence leaves this method;
// resource-qualified state and token readers remain authoritative.
func (m *ChallengeManager) RemoteSessionConnectionsElsewhere(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, userSessionIssuerID uuid.UUID, resource string) (map[uuid.UUID]bool, error) {
	if subject.IsZero() || resource == "" {
		return nil, nil
	}
	rows, err := remotesessionsrepo.New(m.db).ListRemoteSessionStatusesForSubject(ctx, remotesessionsrepo.ListRemoteSessionStatusesForSubjectParams{
		SubjectUrn: subject, ProjectID: projectID, OrganizationID: organizationID, UserSessionIssuerID: userSessionIssuerID, OrganizationWide: false,
	})
	if err != nil {
		return nil, fmt.Errorf("list linked connections elsewhere: %w", err)
	}
	return connectionsElsewhere(rows, resource), nil
}

func connectionsElsewhere(rows []remotesessionsrepo.ListRemoteSessionStatusesForSubjectRow, resource string) map[uuid.UUID]bool {
	found := make(map[uuid.UUID]bool)
	for _, row := range rows {
		if row.Status == string(RemoteSessionActive) && row.Resource.Valid && strings.TrimRight(row.Resource.String, "/") != strings.TrimRight(resource, "/") {
			found[row.RemoteSessionClientID] = true
		}
	}
	return found
}
