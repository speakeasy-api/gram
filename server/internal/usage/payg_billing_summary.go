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

func (s *Service) GetPaygBillingSummary(ctx context.Context, _ *gen.GetPaygBillingSummaryPayload) (*gen.PaygBillingSummary, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}
	if err := s.requirePaygOrganization(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	_, subscription, err := s.getStripeBillingState(ctx, authCtx.ActiveOrganizationID)
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
		return nil, oops.E(oops.CodeConflict, nil, "the live paid Stripe service period is unavailable")
	}

	tumTokens, err := s.getTokensUnderManagementForPeriod(ctx, authCtx.ActiveOrganizationID, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}
	if tumTokens > maxJSONSafeInteger || tumTokens < -maxJSONSafeInteger {
		return nil, oops.E(oops.CodeUnexpected, nil, "tokens under management exceed the safe API range").LogError(ctx, s.logger)
	}

	completedBefore := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	costs, err := s.repo.GetPaygBillingSummaryCosts(ctx, repo.GetPaygBillingSummaryCostsParams{
		TumTokens:       tumTokens,
		TumUnitPriceUsd: TUMUnitPriceUSD,
		OrganizationID:  authCtx.ActiveOrganizationID,
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

	return &gen.PaygBillingSummary{
		PeriodStart:       periodStart.Format(time.RFC3339),
		PeriodEnd:         periodEnd.Format(time.RFC3339),
		TumTokens:         tumTokens,
		TumUnitPriceUsd:   costs.TumUnitPriceUsd,
		TumCostUsd:        costs.TumCostUsd,
		ChatSpendUsd:      costs.ChatSpendUsd,
		RecordedThrough:   recordedThrough,
		EstimatedTotalUsd: costs.EstimatedTotalUsd,
	}, nil
}

func isUTCMidnight(value time.Time) bool {
	value = value.UTC()
	return value.Hour() == 0 && value.Minute() == 0 && value.Second() == 0 && value.Nanosecond() == 0
}

func (s *Service) getTokensUnderManagementForPeriod(ctx context.Context, organizationID string, periodStart, periodEnd time.Time) (int64, error) {
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
		tokens += day.Tokens
	}
	return tokens, nil
}
