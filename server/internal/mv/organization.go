package mv

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/polar"
)

// Necessary to properly populate account type
func DescribeOrganization(ctx context.Context, logger *slog.Logger, orgRepo *orgRepo.Queries, orgID string, polarClient *polar.Client) (*orgRepo.OrganizationMetadatum, error) {
	org, err := orgRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get organization metadata")
	}

	// An org is enterprise iff it's explicitly set to enterprise in the database
	if org.GramAccountType == "enterprise" {
		return &org, nil
	}

	if polarClient == nil {
		logger.WarnContext(ctx, "polar client is not initialized, skipping Polar customer state check")
		return &org, nil
	}

	// This is used during auth, so try to avoid failing 
	customerState, err := polarClient.GetCustomerState(ctx, orgID)
	if err != nil {
		logger.ErrorContext(ctx, "error getting customer state from Polar", attr.SlogError(err)) // TODO: set up an alert for this
		return &org, nil
	}

	// An org is business tier if it has an active Polar subscription
	if customerState != nil && len(customerState.ActiveSubscriptions) > 0 {
		org.GramAccountType = "business"
	}

	return &org, nil
}
