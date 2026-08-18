package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
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

	result := make([]*gen.AdminInferenceKey, len(keys))
	for index, key := range keys {
		result[index] = &gen.AdminInferenceKey{
			KeyType:        key.KeyType,
			MonthlyCredits: key.MonthlyCredits,
			Disabled:       key.Disabled,
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
