package mv

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// Necessary to properly populate account type
func DescribeOrganization(ctx context.Context, logger *slog.Logger, orgRepo *orgRepo.Queries, billingRepo billing.Repository, orgID string) (*orgRepo.OrganizationMetadatum, error) {
	org, err := orgRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get organization metadata")
	}

	// An org is enterprise if it's explicitly set to enterprise in the database
	// if org.GramAccountType == "enterprise" { // TODO
	// 	return &org, nil
	// }

	if billingRepo == nil {
		logger.WarnContext(ctx, "customer provider is not initialized, skipping customer state check")
		return &org, nil
	}

	// This is used during auth, so try to avoid failing
	customerTier, err := billingRepo.GetCustomerTier(ctx, orgID)
	if err != nil {
		logger.ErrorContext(ctx, "error getting customer state", attr.SlogError(err)) // TODO: set up an alert for this
		return &org, nil
	}

	// Otherwise, the source of truth for account type is the Polar customer state
	if customerTier != "" {
		org.GramAccountType = string(customerTier)
	}

	return &org, nil
}
