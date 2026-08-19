package usage

import (
	"context"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

const maxJSONSafeInteger = int64(1<<53 - 1)

type PaygBillingSummary struct {
	PeriodStart            string
	PeriodEnd              string
	TumTokens              int64
	TumUnitPriceUsd        string
	TumCostUsd             string
	OtherInferenceSpendUsd string
	RecordedThrough        *string
	EstimatedTotalUsd      string
}

func (s *Service) GetPaygBillingSummary(ctx context.Context, _ *gen.GetPaygBillingSummaryPayload) (*gen.PaygBillingSummary, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	summary, err := s.GetPaygBillingSummaryForOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}
	return &gen.PaygBillingSummary{
		PeriodStart: summary.PeriodStart, PeriodEnd: summary.PeriodEnd, TumTokens: summary.TumTokens,
		TumUnitPriceUsd: summary.TumUnitPriceUsd, TumCostUsd: summary.TumCostUsd,
		OtherInferenceSpendUsd: summary.OtherInferenceSpendUsd, RecordedThrough: summary.RecordedThrough,
		EstimatedTotalUsd: summary.EstimatedTotalUsd,
	}, nil
}

// GetPaygBillingSummaryForOrganization performs the billing read for an already
// authorized, canonical organization ID. API handlers must authorize callers.
func (s *Service) GetPaygBillingSummaryForOrganization(ctx context.Context, organizationID string) (*PaygBillingSummary, error) {
	if organizationID == "" {
		return nil, oops.C(oops.CodeNotFound)
	}
	if err := s.requirePaygOrganization(ctx, organizationID); err != nil {
		return nil, err
	}

	_, subscription, err := s.getStripeBillingState(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	if subscription.Status != "active" && subscription.Status != "past_due" {
		return nil, oops.E(oops.CodeConflict, nil, "a billing estimate is unavailable before pay as you go billing starts")
	}

	now := time.Now().UTC()
	periodStart := subscription.CurrentPeriodStart.UTC()
	periodEnd := subscription.CurrentPeriodEnd.UTC()
	if periodStart.IsZero() ||
		!periodStart.Before(periodEnd) ||
		!isUTCMidnight(periodStart) ||
		!isUTCMidnight(periodEnd) ||
		now.Before(periodStart) ||
		!now.Before(periodEnd) {
		s.logger.WarnContext(ctx, "live Stripe service period is invalid for PAYG billing summary")
		return nil, oops.E(oops.CodeConflict, nil, "the live paid Stripe service period is unavailable")
	}

	tumTokens, err := s.getTokensUnderManagementForPeriod(ctx, organizationID, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}

	completedBefore := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	costs, err := s.repo.GetPaygBillingSummaryCosts(ctx, repo.GetPaygBillingSummaryCostsParams{
		TumTokens:       tumTokens,
		TumUnitPriceUsd: TUMUnitPriceUSD,
		OrganizationID:  organizationID,
		PeriodStart:     finiteTimestamptz(periodStart),
		PeriodEnd:       finiteTimestamptz(periodEnd),
		CompletedBefore: finiteTimestamptz(completedBefore),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get pay as you go billing summary costs").LogError(ctx, s.logger)
	}

	var recordedThrough *string
	if costs.RecordedThrough.Valid {
		value := costs.RecordedThrough.Time.UTC().Format(time.DateOnly)
		recordedThrough = &value
	}

	return &PaygBillingSummary{
		PeriodStart:            periodStart.Format(time.RFC3339),
		PeriodEnd:              periodEnd.Format(time.RFC3339),
		TumTokens:              tumTokens,
		TumUnitPriceUsd:        costs.TumUnitPriceUsd,
		TumCostUsd:             costs.TumCostUsd,
		OtherInferenceSpendUsd: costs.OtherInferenceSpendUsd,
		RecordedThrough:        recordedThrough,
		EstimatedTotalUsd:      costs.EstimatedTotalUsd,
	}, nil
}

func isUTCMidnight(value time.Time) bool {
	value = value.UTC()
	return value.Hour() == 0 && value.Minute() == 0 && value.Second() == 0 && value.Nanosecond() == 0
}

func (s *Service) getTokensUnderManagementForPeriod(ctx context.Context, organizationID string, periodStart, periodEnd time.Time) (int64, error) {
	if s.telemetryRepo == nil {
		return 0, oops.E(oops.CodeUnavailable, nil, "billing usage telemetry is temporarily unavailable").LogWarn(ctx, s.logger)
	}

	projectIDs, err := s.repo.ListBillingProjectIDsByOrganization(ctx, organizationID)
	if err != nil {
		return 0, oops.E(oops.CodeUnexpected, err, "list organization projects for billing summary").LogError(ctx, s.logger)
	}

	ids := make([]string, 0, len(projectIDs))
	for _, id := range projectIDs {
		ids = append(ids, id.String())
	}
	days, err := s.telemetryRepo.GetTokensUnderManagementByDay(ctx, telemetryrepo.GetTokensUnderManagementParams{
		ProjectIDs:          ids,
		StartUnixNano:       periodStart.UnixNano(),
		EndUnixNano:         periodEnd.UnixNano(),
		ExcludedHookSources: billing.GramHostedHookSourceStrings(),
	})
	if err != nil {
		return 0, oops.E(oops.CodeUnexpected, err, "compute tokens under management for billing summary").LogError(ctx, s.logger)
	}

	var tokens int64
	for _, day := range days {
		if (day.Tokens > 0 && tokens > maxJSONSafeInteger-day.Tokens) ||
			(day.Tokens < 0 && tokens < -maxJSONSafeInteger-day.Tokens) {
			return 0, oops.E(oops.CodeUnexpected, nil, "tokens under management exceed the safe API range").LogError(ctx, s.logger)
		}
		tokens += day.Tokens
	}
	return tokens, nil
}
