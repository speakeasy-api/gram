package activities

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// organizationAdministratorEmails resolves who to email on an organization's
// behalf. A nil client resolves with the engine, as an organization whose
// unit has not moved does; test workers and commands that build no client
// take that path.
func organizationAdministratorEmails(ctx context.Context, client *authz.Client, db *pgxpool.Pool, organizationID string) ([]string, error) {
	if client == nil {
		recipients, err := authz.ResolveOrganizationAdminEmails(ctx, db, organizationID)
		if err != nil {
			return recipients, fmt.Errorf("resolve organization administrators: %w", err)
		}
		return recipients, nil
	}
	recipients, err := client.OrganizationAdministratorEmails(ctx, db, organizationID)
	if err != nil {
		return recipients, fmt.Errorf("resolve organization administrators: %w", err)
	}
	return recipients, nil
}
