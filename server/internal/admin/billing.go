package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/usage"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func (s *Service) GetInferenceKeys(ctx context.Context, payload *gen.GetInferenceKeysPayload) ([]*gen.AdminInferenceKey, error) {
	organizationID, err := s.canonicalAdminOrganizationID(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}

	keyTypes := make([]string, len(openrouter.AllKeyTypes))
	for index, keyType := range openrouter.AllKeyTypes {
		keyTypes[index] = string(keyType)
	}
	keys, err := usagerepo.New(s.db).ListMaterializedOpenRouterInferenceKeys(ctx, usagerepo.ListMaterializedOpenRouterInferenceKeysParams{
		OrganizationID: organizationID,
		KeyTypes:       keyTypes,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list platform-managed inference keys").LogError(ctx, s.logger)
	}

	if len(keys) == 0 {
		return []*gen.AdminInferenceKey{}, nil
	}
	if s.openRouterUsage == nil {
		return nil, oops.E(oops.CodeUnavailable, ErrOpenRouterUnavailable, "OpenRouter usage is temporarily unavailable").LogWarn(ctx, s.logger)
	}

	result := make([]*gen.AdminInferenceKey, len(keys))
	group, groupCtx := errgroup.WithContext(ctx)
	for index, key := range keys {
		keyType := openrouter.KeyType(key.KeyType)
		if err := keyType.Validate(); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "validate stored inference key type").LogError(ctx, s.logger)
		}
		group.Go(func() error {
			creditsUsed, _, err := s.openRouterUsage.GetCreditsUsed(groupCtx, organizationID, keyType)
			if err != nil {
				return fmt.Errorf("read %s inference key usage: %w", keyType, err)
			}
			result[index] = &gen.AdminInferenceKey{
				KeyType:        key.KeyType,
				CreditsUsed:    creditsUsed,
				MonthlyCredits: key.MonthlyCredits,
				Disabled:       key.Disabled,
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		if errors.Is(err, ErrOpenRouterUnavailable) {
			return nil, oops.E(oops.CodeUnavailable, err, "OpenRouter usage is temporarily unavailable").LogWarn(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "read OpenRouter inference key usage").LogError(ctx, s.logger)
	}

	return result, nil
}

func (s *Service) SetInferenceKeyMonthlyLimit(ctx context.Context, payload *gen.SetInferenceKeyMonthlyLimitPayload) (*gen.AdminInferenceKeyLimit, error) {
	keyType := openrouter.KeyType(payload.KeyType)
	if payload.KeyType == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "key_type is required").LogWarn(ctx, s.logger)
	}
	if err := keyType.Validate(); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid inference key type").LogWarn(ctx, s.logger)
	}
	if payload.MonthlyCredits < constants.MinimumPaygSpendCapUSD || payload.MonthlyCredits > constants.MaximumPaygSpendCapUSD {
		return nil, oops.E(oops.CodeInvalid, nil, "monthly_credits must be between %d and %d", constants.MinimumPaygSpendCapUSD, constants.MaximumPaygSpendCapUSD).LogWarn(ctx, s.logger)
	}

	organizationID, err := s.canonicalAdminOrganizationID(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	if s.openRouterSpendCap == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "inference limit updates are temporarily unavailable").LogWarn(ctx, s.logger)
	}

	key, err := usagerepo.New(s.db).GetMaterializedOpenRouterInferenceKey(ctx, usagerepo.GetMaterializedOpenRouterInferenceKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(keyType),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "inference key is not available")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "load inference key before setting monthly limit").LogError(ctx, s.logger)
	case key.Disabled:
		return nil, oops.E(oops.CodeConflict, nil, "the inference key is disabled")
	}

	actor, _ := adminActor(ctx)
	monthlyCredits, err := s.openRouterSpendCap.SetAdminOpenRouterSpendCap(
		ctx,
		uuid.NewString(),
		organizationID,
		keyType,
		payload.MonthlyCredits,
		actor,
		conv.PtrEmpty(audit.SpeakeasyTeamActorLabel),
	)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set inference key monthly limit").LogError(ctx, s.logger)
	}

	return &gen.AdminInferenceKeyLimit{KeyType: string(keyType), MonthlyCredits: int64(monthlyCredits)}, nil
}

func (s *Service) GetInferenceSpendHistory(ctx context.Context, payload *gen.GetInferenceSpendHistoryPayload) ([]*gen.AdminInferenceSpendMonth, error) {
	organizationID, err := s.canonicalAdminOrganizationID(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}

	months, err := usagerepo.New(s.db).ListOpenRouterInferenceSpendByMonth(ctx, usagerepo.ListOpenRouterInferenceSpendByMonthParams{
		OrganizationID:   organizationID,
		BillableKeyTypes: openrouter.BillableKeyTypeStrings(),
		CompletedBefore:  conv.ToPGTimestamptz(time.Now().UTC()),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list inference spend history").LogError(ctx, s.logger)
	}

	result := make([]*gen.AdminInferenceSpendMonth, len(months))
	for index, month := range months {
		result[index] = &gen.AdminInferenceSpendMonth{
			PeriodStart: month.PeriodStart,
			PeriodEnd:   month.PeriodEnd,
			SpendUsd:    month.SpendUsd,
		}
	}
	return result, nil
}

func (s *Service) GetPaygBillingSummary(ctx context.Context, payload *gen.GetPaygBillingSummaryPayload) (*gen.AdminPaygBillingSummary, error) {
	organizationID, err := s.canonicalBillingOrganizationID(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	summary, err := s.billing.GetPaygBillingSummaryForOrganization(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("get PAYG billing summary: %w", err)
	}
	return &gen.AdminPaygBillingSummary{
		PeriodStart: summary.PeriodStart, PeriodEnd: summary.PeriodEnd, TumTokens: summary.TumTokens,
		TumUnitPriceUsd: summary.TumUnitPriceUsd, TumCostUsd: summary.TumCostUsd,
		OtherInferenceSpendUsd: summary.OtherInferenceSpendUsd, RecordedThrough: summary.RecordedThrough,
		EstimatedTotalUsd: summary.EstimatedTotalUsd,
	}, nil
}

func (s *Service) GetStripeSubscription(ctx context.Context, payload *gen.GetStripeSubscriptionPayload) (*gen.AdminStripeSubscription, error) {
	organizationID, err := s.canonicalBillingOrganizationID(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	subscription, err := s.billing.GetStripeSubscriptionForOrganization(ctx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("get Stripe subscription: %w", err)
	}
	return adminStripeSubscription(subscription), nil
}

func (s *Service) CancelStripeSubscription(ctx context.Context, payload *gen.CancelStripeSubscriptionPayload) (*gen.AdminStripeSubscription, error) {
	return s.setStripeSubscriptionCancelAtPeriodEnd(ctx, payload.OrganizationID, true)
}

func (s *Service) ResumeStripeSubscription(ctx context.Context, payload *gen.ResumeStripeSubscriptionPayload) (*gen.AdminStripeSubscription, error) {
	return s.setStripeSubscriptionCancelAtPeriodEnd(ctx, payload.OrganizationID, false)
}

func (s *Service) setStripeSubscriptionCancelAtPeriodEnd(ctx context.Context, requestedOrganizationID string, cancelAtPeriodEnd bool) (*gen.AdminStripeSubscription, error) {
	organizationID, err := s.canonicalBillingOrganizationID(ctx, requestedOrganizationID)
	if err != nil {
		return nil, err
	}
	actor, _ := adminActor(ctx)
	subscription, err := s.billing.SetStripeSubscriptionCancelAtPeriodEndForOrganization(ctx, organizationID, usage.BillingActor{
		Principal: actor, DisplayName: conv.PtrEmpty(audit.SpeakeasyTeamActorLabel),
	}, cancelAtPeriodEnd)
	if err != nil {
		return nil, fmt.Errorf("update Stripe subscription: %w", err)
	}
	return adminStripeSubscription(subscription), nil
}

func (s *Service) canonicalBillingOrganizationID(ctx context.Context, organizationID string) (string, error) {
	if s.billing == nil {
		return "", oops.E(oops.CodeUnavailable, nil, "billing operations are temporarily unavailable").LogWarn(ctx, s.logger)
	}
	return s.canonicalAdminOrganizationID(ctx, organizationID)
}

func (s *Service) canonicalAdminOrganizationID(ctx context.Context, organizationID string) (string, error) {
	organization, err := repo.New(s.db).AdminGetOrganization(ctx, repo.AdminGetOrganizationParams{ID: organizationID, AllowSlug: false})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", oops.C(oops.CodeNotFound)
	case err != nil:
		return "", oops.E(oops.CodeUnexpected, err, "resolve billing organization").LogError(ctx, s.logger)
	default:
		return organization.ID, nil
	}
}

func adminStripeSubscription(value *usage.StripeSubscription) *gen.AdminStripeSubscription {
	return &gen.AdminStripeSubscription{
		Status: value.Status, CurrentPeriodStart: value.CurrentPeriodStart, CurrentPeriodEnd: value.CurrentPeriodEnd,
		TrialStart: value.TrialStart, TrialEnd: value.TrialEnd, CancelAtPeriodEnd: value.CancelAtPeriodEnd,
		CancelAt: value.CancelAt, CanceledAt: value.CanceledAt, PaymentFailed: value.PaymentFailed,
	}
}
