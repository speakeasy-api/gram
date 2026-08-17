package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// CollectOpenRouterDailySpendArgs selects a half-open range of completed UTC
// days. The scheduled workflow overlaps recent runs so upstream restatements
// replace previously collected values.
type CollectOpenRouterDailySpendArgs struct {
	// StartDay is the first UTC day to collect.
	StartDay time.Time

	// EndDay is the exclusive UTC day boundary.
	EndDay time.Time
}

// CollectOpenRouterDailySpend stores billing-grade daily spend for every live
// platform-managed OpenRouter key.
type CollectOpenRouterDailySpend struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	spendClient openrouter.SpendClient
}

func NewCollectOpenRouterDailySpend(
	logger *slog.Logger,
	db *pgxpool.Pool,
	spendClient openrouter.SpendClient,
) *CollectOpenRouterDailySpend {
	return &CollectOpenRouterDailySpend{
		logger:      logger.With(attr.SlogComponent("collect_openrouter_daily_spend")),
		db:          db,
		spendClient: spendClient,
	}
}

func (c *CollectOpenRouterDailySpend) Do(ctx context.Context, args CollectOpenRouterDailySpendArgs) error {
	startDay, err := exactUTCDay(args.StartDay)
	if err != nil {
		return fmt.Errorf("validate openrouter spend start day: %w", err)
	}
	endDay, err := exactUTCDay(args.EndDay)
	if err != nil {
		return fmt.Errorf("validate openrouter spend end day: %w", err)
	}
	if !startDay.Before(endDay) {
		return errors.New("validate openrouter spend range: start day must precede end day")
	}

	queries := repo.New(c.db)
	targets, err := queries.ListOpenRouterDailySpendTargets(ctx)
	if err != nil {
		return fmt.Errorf("list openrouter daily spend targets: %w", err)
	}

	var failures []error
	for i, target := range targets {
		keyType := openrouter.KeyType(target.KeyType)
		if err := keyType.Validate(); err != nil {
			failures = append(failures, fmt.Errorf("collect spend for organization %s: %w", target.OrganizationID, err))
			c.recordHeartbeat(ctx, i+1, len(targets))
			continue
		}

		targetStart := startDay
		createdDay := startOfUTCDay(target.CreatedAt.Time)
		if createdDay.After(targetStart) {
			targetStart = createdDay
		}
		if !targetStart.Before(endDay) {
			c.recordHeartbeat(ctx, i+1, len(targets))
			continue
		}

		// Network access intentionally finishes before the transaction begins.
		// A slow management API must not hold a database connection or locks.
		result, err := c.spendClient.GetDailySpend(ctx, target.KeyHash, targetStart, endDay)
		if err != nil {
			wrapped := fmt.Errorf("collect spend for organization %s key type %s: %w", target.OrganizationID, target.KeyType, err)
			failures = append(failures, wrapped)
			c.logger.ErrorContext(ctx, "collect openrouter daily spend",
				attr.SlogOrganizationID(target.OrganizationID),
				attr.SlogOpenRouterKeyType(target.KeyType),
				attr.SlogError(err),
			)
			c.recordHeartbeat(ctx, i+1, len(targets))
			continue
		}

		spendByDay, err := validateDailySpendResult(result, targetStart, endDay)
		if err != nil {
			failures = append(failures, fmt.Errorf("validate spend for organization %s key type %s: %w", target.OrganizationID, target.KeyType, err))
			c.recordHeartbeat(ctx, i+1, len(targets))
			continue
		}

		if err := c.storeTargetDays(ctx, target.OrganizationID, target.KeyType, targetStart, endDay, spendByDay); err != nil {
			failures = append(failures, fmt.Errorf("store spend for organization %s key type %s: %w", target.OrganizationID, target.KeyType, err))
			c.logger.ErrorContext(ctx, "store openrouter daily spend",
				attr.SlogOrganizationID(target.OrganizationID),
				attr.SlogOpenRouterKeyType(target.KeyType),
				attr.SlogError(err),
			)
		}
		c.recordHeartbeat(ctx, i+1, len(targets))
	}

	// A gap before StartDay is outside the overlapping recovery window. Start
	// from the first stored row so rollout and newly created keys do not alert
	// for history the collector never had an opportunity to fetch.
	for _, target := range targets {
		missing, err := queries.CountOpenRouterDailySpendGaps(ctx, repo.CountOpenRouterDailySpendGapsParams{
			TargetOrganizationID: target.OrganizationID,
			TargetKeyType:        target.KeyType,
			TargetCutoffDay:      pgtype.Date{Time: startDay, InfinityModifier: pgtype.Finite, Valid: true},
		})
		if err != nil {
			failures = append(failures, fmt.Errorf("check spend gaps for organization %s key type %s: %w", target.OrganizationID, target.KeyType, err))
			continue
		}
		if missing > 0 {
			failures = append(failures, fmt.Errorf("organization %s key type %s has %d daily spend gaps before recovery window", target.OrganizationID, target.KeyType, missing))
		}
	}

	return errors.Join(failures...)
}

func (c *CollectOpenRouterDailySpend) storeTargetDays(
	ctx context.Context,
	organizationID string,
	keyType string,
	startDay time.Time,
	endDay time.Time,
	spendByDay map[string]pgtype.Numeric,
) error {
	tx, err := c.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin daily spend transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	txQueries := repo.New(tx)
	for day := startDay; day.Before(endDay); day = day.AddDate(0, 0, 1) {
		spend, ok := spendByDay[day.Format(time.DateOnly)]
		if !ok {
			spend = pgtype.Numeric{Int: nil, Exp: 0, NaN: false, InfinityModifier: pgtype.Finite, Valid: true}
			if err := spend.Scan("0"); err != nil {
				return fmt.Errorf("represent zero daily spend: %w", err)
			}
		}
		if err := txQueries.UpsertOpenRouterDailySpend(ctx, repo.UpsertOpenRouterDailySpendParams{
			TargetOrganizationID: organizationID,
			TargetKeyType:        keyType,
			TargetDay:            pgtype.Date{Time: day, InfinityModifier: pgtype.Finite, Valid: true},
			TargetSpendUsd:       spend,
		}); err != nil {
			return fmt.Errorf("upsert daily spend for %s: %w", day.Format(time.DateOnly), err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit daily spend transaction: %w", err)
	}
	return nil
}

func validateDailySpendResult(result openrouter.DailySpendResult, startDay, endDay time.Time) (map[string]pgtype.Numeric, error) {
	if result.Source != openrouter.DailySpendSourceAnalytics && result.Source != openrouter.DailySpendSourceActivity {
		return nil, fmt.Errorf("unknown spend source %q", result.Source)
	}

	spendByDay := make(map[string]pgtype.Numeric, len(result.Days))
	for _, row := range result.Days {
		day, err := exactUTCDay(row.Day)
		if err != nil {
			return nil, fmt.Errorf("validate result day: %w", err)
		}
		if day.Before(startDay) || !day.Before(endDay) {
			return nil, fmt.Errorf("result day %s is outside requested range", day.Format(time.DateOnly))
		}
		dayKey := day.Format(time.DateOnly)
		if _, exists := spendByDay[dayKey]; exists {
			return nil, fmt.Errorf("result contains duplicate day %s", dayKey)
		}

		var spend pgtype.Numeric
		if err := spend.Scan(strings.TrimSpace(row.SpendUSD)); err != nil || !spend.Valid || spend.NaN || spend.InfinityModifier != pgtype.Finite || spend.Int == nil {
			return nil, fmt.Errorf("result day %s has invalid spend %q", dayKey, row.SpendUSD)
		}
		if spend.Int.Sign() < 0 {
			return nil, fmt.Errorf("result day %s has negative spend", dayKey)
		}
		if err := validateDailySpendNumeric(spend); err != nil {
			return nil, fmt.Errorf("result day %s: %w", dayKey, err)
		}
		spendByDay[dayKey] = spend
	}
	return spendByDay, nil
}

// validateDailySpendNumeric ensures PostgreSQL NUMERIC(14,6) can persist the
// upstream decimal without rounding or overflowing. Multiplying by 10^6 must
// produce an integer no larger than the column's 14-digit scaled maximum.
func validateDailySpendNumeric(spend pgtype.Numeric) error {
	scaled := new(big.Int).Set(spend.Int)
	scaledExponent := int64(spend.Exp) + 6
	if scaledExponent >= 0 {
		scaled.Mul(scaled, new(big.Int).Exp(big.NewInt(10), big.NewInt(scaledExponent), nil))
	} else {
		divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(-scaledExponent), nil)
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(scaled, divisor, remainder)
		if remainder.Sign() != 0 {
			return errors.New("spend has more than 6 non-zero fractional digits")
		}
		scaled = quotient
	}

	maxScaled := new(big.Int).Sub(new(big.Int).Exp(big.NewInt(10), big.NewInt(14), nil), big.NewInt(1))
	if scaled.Cmp(maxScaled) > 0 {
		return errors.New("spend exceeds NUMERIC(14,6) precision")
	}
	return nil
}

func exactUTCDay(value time.Time) (time.Time, error) {
	day := startOfUTCDay(value)
	if !value.Equal(day) {
		return time.Time{}, fmt.Errorf("%s is not a UTC midnight", value.Format(time.RFC3339))
	}
	return day, nil
}

func startOfUTCDay(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func (c *CollectOpenRouterDailySpend) recordHeartbeat(ctx context.Context, processed, total int) {
	if activity.IsActivity(ctx) {
		activity.RecordHeartbeat(ctx, map[string]int{"processed": processed, "total": total})
	}
}
