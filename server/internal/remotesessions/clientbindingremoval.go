package remotesessions

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/conv"

	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// clientBindingServers is, for each project with a server on one of a
// client's actual server associations or typed offers, the organization and those issuers.
type clientBindingServers struct {
	organizationID string
	projectID      uuid.UUID
	issuerIDs      []uuid.UUID
}

// ClientBindingServers is every server project an actual registration association or offer reaches,
// read before a writer retires the registration.
type ClientBindingServers []clientBindingServers

// ReadClientBindingServers reads the projects reached by clientID's associations and offers in organizationID. Read it before the transaction's writer
// scope, and lock Projects with it.
func ReadClientBindingServers(ctx context.Context, db repo.DBTX, organizationID string, clientID uuid.UUID) (ClientBindingServers, error) {
	return readClientBindingServers(ctx, db, conv.ToPGText(organizationID), clientID)
}

// ReadGlobalClientBindingServers is ReadClientBindingServers for a
// platform-global client, whose associations and offers reach servers in any organization.
func ReadGlobalClientBindingServers(ctx context.Context, db repo.DBTX, clientID uuid.UUID) (ClientBindingServers, error) {
	return readClientBindingServers(ctx, db, pgtype.Text{String: "", Valid: false}, clientID)
}

func readClientBindingServers(ctx context.Context, db repo.DBTX, organizationID pgtype.Text, clientID uuid.UUID) (ClientBindingServers, error) {
	rows, err := repo.New(db).ListServerProjectsOnClientIssuers(ctx, repo.ListServerProjectsOnClientIssuersParams{
		RemoteSessionClientID: clientID,
		OrganizationID:        organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("list servers on the client's issuers: %w", err)
	}
	var servers ClientBindingServers
	for _, row := range rows {
		if n := len(servers); n > 0 && servers[n-1].organizationID == row.OrganizationID && servers[n-1].projectID == row.ProjectID {
			servers[n-1].issuerIDs = append(servers[n-1].issuerIDs, row.UserSessionIssuerID.UUID)
			continue
		}
		servers = append(servers, clientBindingServers{organizationID: row.OrganizationID, projectID: row.ProjectID, issuerIDs: []uuid.UUID{row.UserSessionIssuerID.UUID}})
	}
	return servers, nil
}

// Organizations lists the organizations whose servers the registration reaches.
func (s ClientBindingServers) Organizations() []string {
	var organizations []string
	for _, project := range s {
		if !slices.Contains(organizations, project.organizationID) {
			organizations = append(organizations, project.organizationID)
		}
	}
	return organizations
}

// Projects lists the projects of organizationID whose servers the bindings
// reach, for the writer scope that must cover Derive.
func (s ClientBindingServers) Projects(organizationID string) []uuid.UUID {
	var projects []uuid.UUID
	for _, project := range s {
		if project.organizationID == organizationID {
			projects = append(projects, project.projectID)
		}
	}
	return projects
}

// Derive recomputes the upstream issuer of the servers the deleted registration
// reached. It rewrites only that derived column: the writer has already
// pruned the client's associations and synchronized their typed records, and
// holds the writer scope of every project in Projects.
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
