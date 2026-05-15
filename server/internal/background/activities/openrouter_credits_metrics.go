package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	repo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const (
	meterOpenRouterCreditsRemaining = "gram.openrouter.credits_remaining"
	meterOpenRouterCreditsUsedRatio = "gram.openrouter.credits_used_ratio"

	openRouterCreditsPollConcurrency = 10
)

type CollectOpenRouterCreditsMetrics struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	repo       *repo.Queries
	openRouter openrouter.Provisioner
}

func NewCollectOpenRouterCreditsMetrics(
	logger *slog.Logger,
	db *pgxpool.Pool,
	openRouterProvisioner openrouter.Provisioner,
) *CollectOpenRouterCreditsMetrics {
	return &CollectOpenRouterCreditsMetrics{
		logger:     logger.With(attr.SlogComponent("collect_openrouter_credits_metrics")),
		db:         db,
		repo:       repo.New(db),
		openRouter: openRouterProvisioner,
	}
}

type OpenRouterCreditsMetric struct {
	OrganizationID   string
	OrganizationSlug string
	AccountType      string
	CreditsUsed      float64
	CreditLimit      int64
}

type CollectOpenRouterCreditsMetricsArgs struct {
	// AccountTypes is the allow-list of `organization_metadata.gram_account_type`
	// values whose OpenRouter keys should be polled. Expand here (e.g. add
	// "pro") to grow coverage without code changes elsewhere.
	AccountTypes []string
}

func (c *CollectOpenRouterCreditsMetrics) Do(ctx context.Context, args CollectOpenRouterCreditsMetricsArgs) ([]OpenRouterCreditsMetric, error) {
	rows, err := c.repo.GetOpenRouterCreditsMonitoringTargets(ctx, args.AccountTypes)
	if err != nil {
		return nil, fmt.Errorf("list openrouter credits monitoring targets: %w", err)
	}

	// Pre-allocate and write to a disjoint index per goroutine — no mutex
	// needed. Failed polls leave their slot zero-valued and are filtered out
	// of the final result.
	results := make([]OpenRouterCreditsMetric, len(rows))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(openRouterCreditsPollConcurrency)
	for i, row := range rows {
		g.Go(func() error {
			used, err := c.openRouter.GetKeyUsage(gctx, row.ApiKey)
			if err != nil {
				// Skip on a per-org failure so one bad key does not blank the
				// whole batch. The error is logged for diagnosis and swallowed
				// so the errgroup does not cancel sibling polls.
				c.logger.ErrorContext(gctx, "fetch openrouter key usage",
					attr.SlogOrganizationID(row.OrganizationID),
					attr.SlogOrganizationSlug(row.OrganizationSlug),
					attr.SlogError(err),
				)
				return nil
			}

			results[i] = OpenRouterCreditsMetric{
				OrganizationID:   row.OrganizationID,
				OrganizationSlug: row.OrganizationSlug,
				AccountType:      row.GramAccountType,
				CreditsUsed:      used,
				CreditLimit:      row.MonthlyCredits,
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("wait for openrouter credits polls: %w", err)
	}

	collected := results[:0]
	for _, r := range results {
		if r.OrganizationID != "" {
			collected = append(collected, r)
		}
	}
	return collected, nil
}

type FireOpenRouterCreditsMetrics struct {
	logger           *slog.Logger
	creditsRemaining metric.Float64Gauge
	creditsUsedRatio metric.Float64Gauge
}

func NewFireOpenRouterCreditsMetrics(logger *slog.Logger, meterProvider metric.MeterProvider) *FireOpenRouterCreditsMetrics {
	ctx := context.Background()
	componentLogger := logger.With(attr.SlogComponent("fire_openrouter_credits_metrics"))

	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/background/activities/openrouter_credits")

	remaining, err := meter.Float64Gauge(
		meterOpenRouterCreditsRemaining,
		metric.WithDescription("Remaining OpenRouter monthly credits per org (limit minus usage)."),
		metric.WithUnit("{credit}"),
	)
	if err != nil {
		componentLogger.ErrorContext(ctx, "create metric",
			attr.SlogMetricName(meterOpenRouterCreditsRemaining), attr.SlogError(err))
	}

	usedRatio, err := meter.Float64Gauge(
		meterOpenRouterCreditsUsedRatio,
		metric.WithDescription("Fraction of monthly OpenRouter credits used per org (0.0–1.0)."),
		metric.WithUnit("1"),
	)
	if err != nil {
		componentLogger.ErrorContext(ctx, "create metric",
			attr.SlogMetricName(meterOpenRouterCreditsUsedRatio), attr.SlogError(err))
	}

	return &FireOpenRouterCreditsMetrics{
		logger:           componentLogger,
		creditsRemaining: remaining,
		creditsUsedRatio: usedRatio,
	}
}

func (f *FireOpenRouterCreditsMetrics) Do(ctx context.Context, metrics []OpenRouterCreditsMetric) error {
	for _, m := range metrics {
		attrs := metric.WithAttributes(
			attr.OrganizationID(m.OrganizationID),
			attr.OrganizationSlug(m.OrganizationSlug),
			attr.OrganizationAccountType(m.AccountType),
		)

		if f.creditsRemaining != nil {
			f.creditsRemaining.Record(ctx, float64(m.CreditLimit)-m.CreditsUsed, attrs)
		}

		// Skip ratio when limit is zero — gives no useful signal and would
		// divide by zero. Disabled/unprovisioned keys are already filtered
		// at the SQL layer; this guards against a 0-limit edge case.
		if f.creditsUsedRatio != nil && m.CreditLimit > 0 {
			f.creditsUsedRatio.Record(ctx, m.CreditsUsed/float64(m.CreditLimit), attrs)
		}
	}
	return nil
}
