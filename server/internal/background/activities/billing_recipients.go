package activities

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/billingnotifications"
)

// recipientEmailIdempotencyKey keeps recipient-aware Loops keys under its
// 100-character limit and makes address deduplication case-insensitive.
func recipientEmailIdempotencyKey(recipient string, parts ...string) string {
	return billingnotifications.RecipientIdempotencyKey(recipient, parts...)
}

func resolveBillingNotificationRecipients(
	ctx context.Context,
	db repo.DBTX,
	organizationID string,
	accountType string,
	configuredEmail *string,
) ([]string, error) {
	recipients, err := billingnotifications.ResolveRecipients(ctx, db, organizationID, accountType, configuredEmail)
	if err != nil {
		return recipients, fmt.Errorf("resolve billing notification recipients: %w", err)
	}
	return recipients, nil
}
