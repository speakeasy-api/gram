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

	customerTier, hasActiveSubscription := ResolveOrganizationTier(ctx, logger, billingRepo, orgID, billing.Tier(previousAccountType))
	org.HasActiveSubscription = hasActiveSubscription

	// Reconcile provider-managed tiers without overwriting concurrent updates.
	if previousAccountType != string(customerTier) {
		updated, err := orgRepo.SetAccountTypeIfUnchanged(ctx, org_repo.SetAccountTypeIfUnchangedParams{
			GramAccountType:     string(customerTier),
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
			org.GramAccountType = string(customerTier)
		}
	}

	return &org, nil
}

// ResolveOrganizationTier resolves the current billing tier without loading or
// reconciling organization metadata. Operator-managed tiers are authoritative;
// unavailable billing state falls back to the persisted tier.
func ResolveOrganizationTier(ctx context.Context, logger *slog.Logger, billingRepo billing.Repository, orgID string, persistedTier billing.Tier) (billing.Tier, bool) {
	if isOperatorManagedTier(persistedTier) {
		return persistedTier, true
	}

	if billingRepo == nil {
		logger.WarnContext(ctx, "customer provider is not initialized, skipping customer state check")
		return persistedTier, false
	}

	// This is used during auth, so try to avoid failing.
	customerTier, hasActiveSubscription, err := billingRepo.GetCustomerTier(ctx, orgID)
	if err != nil {
		logger.ErrorContext(ctx, "error getting customer state", attr.SlogError(err))
		return persistedTier, false
	}
	if customerTier == nil {
		return persistedTier, hasActiveSubscription
	}

	return *customerTier, hasActiveSubscription
}

func isOperatorManagedTier(tier billing.Tier) bool {
	return tier == billing.TierEnterprise || tier == billing.TierPayg
}
