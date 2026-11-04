package billingnotifications

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
)

// RecipientIdempotencyKey keeps recipient-aware provider keys short and makes
// address identity case-insensitive.
func RecipientIdempotencyKey(recipient string, parts ...string) string {
	parts = append(parts, strings.ToLower(recipient))
	digest := sha256.Sum256([]byte(strings.Join(parts, ":")))
	return hex.EncodeToString(digest[:])
}

// ResolveRecipients applies the product billing-email policy for one tier.
// PAYG organizations without a configured address fall back to their
// administrators, resolved through client. A nil client resolves with the
// engine, as an organization whose unit has not moved does.
func ResolveRecipients(ctx context.Context, client *authz.Client, db repo.DBTX, organizationID, accountType string, configuredEmail *string) ([]string, error) {
	switch billing.Tier(accountType) {
	case billing.TierEnterprise:
		if configuredEmail == nil {
			return nil, nil
		}
		return []string{*configuredEmail}, nil
	case billing.TierPayg:
		if configuredEmail != nil {
			return []string{*configuredEmail}, nil
		}
		recipients, err := OrganizationAdministratorEmails(ctx, client, db, organizationID)
		if err != nil {
			return recipients, fmt.Errorf("resolve PAYG organization administrator recipients: %w", err)
		}
		return recipients, nil
	default:
		return nil, nil
	}
}

// OrganizationAdministratorEmails resolves who to email on an organization's
// behalf. A nil client resolves with the engine, as an organization whose unit
// has not moved does; services built without a client take that path.
func OrganizationAdministratorEmails(ctx context.Context, client *authz.Client, db repo.DBTX, organizationID string) ([]string, error) {
	if client == nil {
		recipients, err := authz.ResolveOrganizationAdminEmails(ctx, db, organizationID)
		if err != nil {
			return recipients, fmt.Errorf("resolve with engine: %w", err)
		}
		return recipients, nil
	}
	recipients, err := client.OrganizationAdministratorEmails(ctx, db, organizationID)
	if err != nil {
		return recipients, fmt.Errorf("resolve with authorization client: %w", err)
	}
	return recipients, nil
}
