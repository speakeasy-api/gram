package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/metering/chrepo"
)

const usageSummaryRebuildLockName = "gram-demo-seed"

// RebuildUsageSummaries rebuilds and atomically publishes the complete retained
// usage-summary generation. The shared session lock prevents publication from
// overlapping a demo seed's raw-ledger replacement.
type RebuildUsageSummaries struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	ch     clickhouse.Conn
}

func NewRebuildUsageSummaries(logger *slog.Logger, db *pgxpool.Pool, ch clickhouse.Conn) *RebuildUsageSummaries {
	return &RebuildUsageSummaries{logger: logger, db: db, ch: ch}
}

func (a *RebuildUsageSummaries) Do(ctx context.Context, snapshotAt time.Time) (retErr error) {
	conn, err := a.db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire usage summary rebuild lock connection: %w", err)
	}

	var locked bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtext($1))", usageSummaryRebuildLockName).Scan(&locked); err != nil {
		// Cancellation can surface after PostgreSQL acquired the session lock.
		// Destroy the uncertain session instead of returning it to the pool.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if closeErr := conn.Hijack().Close(cleanupCtx); closeErr != nil {
			a.logger.ErrorContext(cleanupCtx, "close uncertain usage summary rebuild lock session", attr.SlogError(closeErr))
		}
		return fmt.Errorf("acquire usage summary rebuild advisory lock: %w", err)
	}
	if !locked {
		conn.Release()
		a.logger.InfoContext(ctx, "skipping usage summary rebuild because shared lock is busy")
		return nil
	}

	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		var unlocked bool
		unlockErr := conn.QueryRow(unlockCtx, "SELECT pg_advisory_unlock(hashtext($1))", usageSummaryRebuildLockName).Scan(&unlocked)
		if unlockErr == nil && unlocked {
			conn.Release()
			return
		}
		if unlockErr == nil {
			unlockErr = errors.New("usage summary rebuild advisory lock was not held by this session")
		}
		a.logger.ErrorContext(unlockCtx, "release usage summary rebuild advisory lock", attr.SlogError(unlockErr))
		if closeErr := conn.Hijack().Close(unlockCtx); closeErr != nil {
			a.logger.ErrorContext(unlockCtx, "close usage summary rebuild lock session", attr.SlogError(closeErr))
		}
		retErr = errors.Join(retErr, fmt.Errorf("release usage summary rebuild advisory lock: %w", unlockErr))
	}()

	if err := chrepo.New(a.ch).RebuildUsageSummaries(ctx, snapshotAt.UTC()); err != nil {
		return fmt.Errorf("rebuild usage summaries: %w", err)
	}
	return nil
}
