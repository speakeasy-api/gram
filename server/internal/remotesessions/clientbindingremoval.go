package remotesessions

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// clientBindingServers is, for each project with a server on one of a
// client's user session issuers, the organization and those issuers.
type clientBindingServers struct {
	organizationID string
	projectID      uuid.UUID
	issuerIDs      []uuid.UUID
}

// ClientBindingServers is every server project a client's issuer bindings
// reach, read before a writer deletes those bindings.
type ClientBindingServers []clientBindingServers

// ReadClientBindingServers reads the server projects clientID's issuer
// bindings reach, in any organization.
func ReadClientBindingServers(ctx context.Context, db repo.DBTX, clientID uuid.UUID) (ClientBindingServers, error) {
	rows, err := repo.New(db).ListServerProjectsOnClientIssuers(ctx, clientID)
	if err != nil {
		return nil, fmt.Errorf("list servers on the client's issuers: %w", err)
	}
	var servers ClientBindingServers
	for _, row := range rows {
		if n := len(servers); n > 0 && servers[n-1].organizationID == row.OrganizationID && servers[n-1].projectID == row.ProjectID {
			servers[n-1].issuerIDs = append(servers[n-1].issuerIDs, row.UserSessionIssuerID)
			continue
		}
		servers = append(servers, clientBindingServers{organizationID: row.OrganizationID, projectID: row.ProjectID, issuerIDs: []uuid.UUID{row.UserSessionIssuerID}})
	}
	return servers, nil
}

// Derive recomputes the upstream issuer of the servers the deleted bindings
// reached. It rewrites only that derived column: the writer has already
// pruned the client's associations and synchronized their typed records.
func (s ClientBindingServers) Derive(ctx context.Context, db mcpserversrepo.DBTX) error {
	q := mcpserversrepo.New(db)
	for _, project := range s {
		if _, err := q.ResyncMCPServerRemoteSessionIssuers(ctx, mcpserversrepo.ResyncMCPServerRemoteSessionIssuersParams{
			UserSessionIssuerIds: project.issuerIDs,
			OrganizationID:       project.organizationID,
			ProjectID:            project.projectID,
		}); err != nil {
			return fmt.Errorf("derive upstream issuer of servers on the client's issuers: %w", err)
		}
	}
	return nil
}
