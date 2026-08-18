package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/attr"
	repo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
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
	enc        *encryption.Client
}

func NewCollectOpenRouterCreditsMetrics(
	logger *slog.Logger,
	db *pgxpool.Pool,
	openRouterProvisioner openrouter.Provisioner,
	enc *encryption.Client,
) *CollectOpenRouterCreditsMetrics {
	return &CollectOpenRouterCreditsMetrics{
		logger:     logger.With(attr.SlogComponent("collect_openrouter_credits_metrics")),
		db:         db,
		repo:       repo.New(db),
		openRouter: openRouterProvisioner,
		enc:        enc,
	}
}

type OpenRouterCreditsMetric struct {
	OrganizationID   string
	OrganizationSlug string
	AccountType      string
	// KeyType distinguishes the chat and internal key series; without it the
	// two rows per org would overwrite each other's gauges.
	KeyType     string
	CreditsUsed float64
	CreditLimit int64
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
			// Resolution failures are logged and skipped so one bad row does
			// not blank the batch.
			if !row.ApiKeyEncrypted.Valid {
				c.logger.ErrorContext(gctx, "openrouter key row holds no encrypted key material",
					attr.SlogOrganizationID(row.OrganizationID),
					attr.SlogOrganizationSlug(row.OrganizationSlug),
				)
				return nil
			}
			apiKey, decErr := c.enc.Decrypt(row.ApiKeyEncrypted.String)
			if decErr != nil {
				c.logger.ErrorContext(gctx, "decrypt openrouter key for usage polling",
					attr.SlogOrganizationID(row.OrganizationID),
					attr.SlogOrganizationSlug(row.OrganizationSlug),
					attr.SlogError(decErr),
				)
				return nil
			}
			if apiKey == "" {
				c.logger.ErrorContext(gctx, "openrouter key row holds no key material",
					attr.SlogOrganizationID(row.OrganizationID),
					attr.SlogOrganizationSlug(row.OrganizationSlug),
				)
				return nil
			}

			keyType := openrouter.KeyType(row.KeyType)
			if err := withOpenRouterKeyBillingConnectionLock(gctx, c.logger, c.db, row.OrganizationID, keyType, func(conn *pgxpool.Conn, _ *repo.Queries) error {
				dbProvisioner, ok := c.openRouter.(openRouterKeyBillingDBProvisioner)
				if !ok {
					return errors.New("OpenRouter key provisioner cannot use the locked database session")
				}
				currentKey, err := openrouterrepo.New(conn).GetOpenRouterAPIKey(gctx, openrouterrepo.GetOpenRouterAPIKeyParams{
					OrganizationID: row.OrganizationID,
					KeyType:        row.KeyType,
				})
				if err != nil {
					return fmt.Errorf("refresh openrouter key snapshot: %w", err)
				}
				if currentKey.Disabled {
					return nil
				}

				// Re-read the key after taking the lock, then keep the lock across the
				// upstream read and mirror reconciliation. A batch snapshot taken
				// before a cap PATCH therefore cannot overwrite the newer cap or erase
				// the generation used to finish the operation's audit.
				used, upstreamLimit, err := c.openRouter.GetKeyUsage(gctx, apiKey)
				if err != nil {
					return fmt.Errorf("fetch openrouter key usage: %w", err)
				}
				effectiveLimit, err := dbProvisioner.ReconcileMonthlyCreditsWithDB(gctx, conn, row.OrganizationID, keyType, currentKey.MonthlyCredits, upstreamLimit)
				if err != nil {
					return fmt.Errorf("reconcile openrouter monthly credits: %w", err)
				}

				results[i] = OpenRouterCreditsMetric{
					OrganizationID:   row.OrganizationID,
					OrganizationSlug: row.OrganizationSlug,
					AccountType:      row.GramAccountType,
					KeyType:          row.KeyType,
					CreditsUsed:      used,
					CreditLimit:      effectiveLimit,
				}
				return nil
			}); err != nil {
				// Skip on a per-key failure so one bad row does not blank the
				// whole batch. The next five-minute tick retries it.
				c.logger.ErrorContext(gctx, "collect openrouter key credits",
					attr.SlogOrganizationID(row.OrganizationID),
					attr.SlogOrganizationSlug(row.OrganizationSlug),
					attr.SlogError(err),
				)
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
			attr.OpenRouterKeyType(m.KeyType),
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
