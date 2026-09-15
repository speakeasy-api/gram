package mv

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/oops"
	org_repo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

type OrganizationDescription struct {
	org_repo.OrganizationMetadatum
	HasActiveSubscription bool
}

// Necessary to properly populate account type
func DescribeOrganization(ctx context.Context, logger *slog.Logger, orgRepo *org_repo.Queries, billingRepo billing.Repository, orgID string) (*OrganizationDescription, error) {
	orgMetadata, err := orgRepo.GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get organization metadata")
	}
	previousAccountType := orgMetadata.GramAccountType

	org := OrganizationDescription{
		OrganizationMetadatum: orgMetadata,
		HasActiveSubscription: false,
	}

	// Operator-managed tiers are authoritative and must never be overwritten by
	// stale billing provider state.
	if isOperatorManagedTier(billing.Tier(org.GramAccountType)) {
		org.HasActiveSubscription = true
		return &org, nil
	}

	if billingRepo == nil {
		logger.WarnContext(ctx, "customer provider is not initialized, skipping customer state check")
		return &org, nil
	}

	// This is used during auth, so try to avoid failing
	customerTier, hasActiveSubscription, err := billingRepo.GetCustomerTier(ctx, orgID)
	if err != nil {
		logger.ErrorContext(ctx, "error getting customer state", attr.SlogError(err)) // TODO: set up an alert for this
		return &org, nil
	}

	org.HasActiveSubscription = hasActiveSubscription

	// Otherwise, the source of truth for account type is the Polar customer state
	if customerTier != nil {
		if previousAccountType != string(*customerTier) {
			updated, err := orgRepo.SetAccountTypeIfUnchanged(ctx, org_repo.SetAccountTypeIfUnchangedParams{
				GramAccountType:     string(*customerTier),
				PreviousAccountType: previousAccountType,
				ID:                  orgID,
			})
			switch {
			case err == nil:
				org.OrganizationMetadatum = updated
			case errors.Is(err, pgx.ErrNoRows):
				current, reloadErr := orgRepo.GetOrganizationMetadata(ctx, orgID)
				if reloadErr != nil {
					logger.ErrorContext(ctx, "error reloading account type after concurrent update", attr.SlogError(reloadErr))
				} else {
					org.OrganizationMetadatum = current
					org.HasActiveSubscription = hasActiveSubscription || isOperatorManagedTier(billing.Tier(current.GramAccountType))
				}
			default:
				logger.ErrorContext(ctx, "error setting account type", attr.SlogError(err))
				org.GramAccountType = string(*customerTier)
			}
		} else {
			org.GramAccountType = string(*customerTier)
		}
	}

	return &org, nil
}

func isOperatorManagedTier(tier billing.Tier) bool {
	return tier == billing.TierEnterprise || tier == billing.TierPayg
}
