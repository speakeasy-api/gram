package activities

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

// recipientEmailIdempotencyKey keeps recipient-aware Loops keys under its
// 100-character limit and makes address deduplication case-insensitive.
func recipientEmailIdempotencyKey(recipient string, parts ...string) string {
	parts = append(parts, strings.ToLower(recipient))
	digest := sha256.Sum256([]byte(strings.Join(parts, ":")))
	return hex.EncodeToString(digest[:])
}

func resolveBillingNotificationRecipients(
	ctx context.Context,
	db repo.DBTX,
	organizationID string,
	accountType string,
	configuredEmail *string,
) ([]string, error) {
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
		recipients, err := authz.ResolveOrganizationAdminEmails(ctx, db, organizationID)
		if err != nil {
			return recipients, fmt.Errorf("resolve PAYG organization administrator recipients: %w", err)
		}
		return recipients, nil
	default:
		return nil, nil
	}
}
