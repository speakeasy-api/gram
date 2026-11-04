package activities

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billingnotifications"
)

// organizationAdministratorEmails resolves who to email on an organization's
// behalf. billingnotifications owns the policy, including the engine fallback
// for test workers and commands that build no client.
func organizationAdministratorEmails(ctx context.Context, client *authz.Client, db *pgxpool.Pool, organizationID string) ([]string, error) {
	recipients, err := billingnotifications.OrganizationAdministratorEmails(ctx, client, db, organizationID)
	if err != nil {
		return recipients, fmt.Errorf("resolve organization administrators: %w", err)
	}
	return recipients, nil
}
