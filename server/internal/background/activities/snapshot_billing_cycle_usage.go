package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/email"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/usage"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

const (
	// snapshotBillingCycles is how many trailing billing cycles (including the
	// active one) are kept refreshed in Postgres. Matches the TUM endpoint's
	// reporting window and is bounded by the 2-year retention of
	// chat_token_summaries, so the first run backfills the full window.
	snapshotBillingCycles = 12

	// billingCycleFinalizeGrace is how long after a cycle closes its snapshot
	// keeps being refreshed before it is finalized and becomes immutable.
	// Telemetry can arrive late (buffered OTEL exports, retried ingest), so a
	// cycle is not sealed at the moment it ends.
	billingCycleFinalizeGrace = 72 * time.Hour
)

// SnapshotBillingCycleUsage persists per-billing-cycle "tokens under
// management" totals from ClickHouse into the durable billing_cycle_usage
// Postgres table. ClickHouse aggregates expire and cannot be rebuilt once the
// raw logs are gone, so these snapshots are the permanent billing record.
type SnapshotBillingCycleUsage struct {
	logger        *slog.Logger
	db            *pgxpool.Pool
	telemetryRepo *telemetryrepo.Queries
	cache         cache.Cache
	emails        *email.Service
}

func NewSnapshotBillingCycleUsage(logger *slog.Logger, db *pgxpool.Pool, chConn clickhouse.Conn, cacheAdapter cache.Cache, emails *email.Service) *SnapshotBillingCycleUsage {
	return &SnapshotBillingCycleUsage{
		logger:        logger,
		db:            db,
		telemetryRepo: telemetryrepo.New(chConn),
		cache:         cacheAdapter,
		emails:        emails,
	}
}

func (s *SnapshotBillingCycleUsage) Do(ctx context.Context, orgIDs []string) error {
	queries := usagerepo.New(s.db)
	now := time.Now().UTC()

	var errs []error
	for _, orgID := range orgIDs {
		if err := s.snapshotOrganization(ctx, queries, orgID, now); err != nil {
			s.logger.ErrorContext(ctx, "failed to snapshot billing cycle usage",
				attr.SlogOrganizationID(orgID), attr.SlogError(err))
			errs = append(errs, fmt.Errorf("snapshot org %s: %w", orgID, err))
		}
	}

	return errors.Join(errs...)
}

func (s *SnapshotBillingCycleUsage) snapshotOrganization(ctx context.Context, queries *usagerepo.Queries, orgID string, now time.Time) error {
	meta, err := queries.GetBillingMetadata(ctx, orgID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get billing metadata: %w", err)
	}
	// On pgx.ErrNoRows the zero-value row stands in for an unconfigured
	// contract; BillingCycles treats anchor day 0 as 1.

	projectIDs, err := queries.ListBillingProjectIDsByOrganization(ctx, orgID)
	if err != nil {
		return fmt.Errorf("list organization projects: %w", err)
	}
	if len(projectIDs) == 0 {
		return nil
	}

	ids := make([]string, 0, len(projectIDs))
	for _, id := range projectIDs {
		ids = append(ids, id.String())
	}

	finalizedRows, err := queries.ListFinalizedBillingCycleStarts(ctx, orgID)
	if err != nil {
		return fmt.Errorf("list finalized billing cycles: %w", err)
	}
	finalized := make(map[time.Time]bool, len(finalizedRows))
	for _, ts := range finalizedRows {
		finalized[ts.Time.UTC()] = true
	}

	// After the initial backfill only the active cycle and any recently closed
	// cycle inside the grace window remain non-finalized, so steady-state runs
	// touch one or two cycles per organization.
	cycles := usage.BillingCycles(now, int(meta.BillingCycleAnchorDay), snapshotBillingCycles)
	// Newest-first: the active and just-closed cycles — the ones the grace
	// window depends on — persist before the historical backfill, in case the
	// activity deadline cuts the loop short. Progress is durable either way:
	// each cycle upserts immediately and finalized cycles skip on retry.
	slices.Reverse(cycles)
	for _, cycle := range cycles {
		if finalized[cycle.Start] {
			continue
		}

		days, err := s.telemetryRepo.GetTokensUnderManagementByDay(ctx, telemetryrepo.GetTokensUnderManagementParams{
			ProjectIDs:    ids,
			StartUnixNano: cycle.Start.UnixNano(),
			EndUnixNano:   cycle.End.UnixNano(),
		})
		if err != nil {
			return fmt.Errorf("compute tokens under management: %w", err)
		}

		var tokens int64
		for _, day := range days {
			tokens += day.Tokens
		}

		finalizedAt := pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite}
		if now.After(cycle.End.Add(billingCycleFinalizeGrace)) {
			finalizedAt = pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite}
		}

		if err := queries.UpsertBillingCycleUsage(ctx, usagerepo.UpsertBillingCycleUsageParams{
			OrganizationID: orgID,
			CycleStart:     pgtype.Timestamptz{Time: cycle.Start, Valid: true, InfinityModifier: pgtype.Finite},
			CycleEnd:       pgtype.Timestamptz{Time: cycle.End, Valid: true, InfinityModifier: pgtype.Finite},
			TumTokens:      tokens,
			FinalizedAt:    finalizedAt,
		}); err != nil {
			return fmt.Errorf("upsert billing cycle usage: %w", err)
		}

		// Threshold alerts only ever concern the cycle in progress; closed
		// cycles inside the grace window refresh silently.
		if !now.Before(cycle.Start) && now.Before(cycle.End) {
			s.maybeSendUsageAlert(ctx, queries, orgID, meta, cycle, tokens, now)
		}
	}

	return nil
}
