package outbox_relay

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
)

const meterOutboxGCDeletedRows = "outbox.gc.deleted_rows"

// GC hard-deletes terminal outbox rows (processed, noop, or dead-lettered)
// older than a configurable retention period.
type GC struct {
	db          *pgxpool.Pool
	deletedRows metric.Int64Counter
}

func NewGC(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool) *GC {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/background/activities/outbox_relay")
	deletedRows, err := meter.Int64Counter(
		meterOutboxGCDeletedRows,
		metric.WithDescription("Number of terminal outbox rows hard-deleted by the GC job"),
		metric.WithUnit("{row}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric error", attr.SlogMetricName(meterOutboxGCDeletedRows), attr.SlogError(err))
	}
	return &GC{db: db, deletedRows: deletedRows}
}

func (g *GC) DeleteProcessedRows(ctx context.Context, cutoff time.Time, batchSize int32) (int64, error) {
	q := repo.New(g.db)
	n, err := q.GCProcessedOutboxRows(ctx, repo.GCProcessedOutboxRowsParams{
		Cutoff:    pgtype.Timestamptz{Time: cutoff, InfinityModifier: pgtype.Finite, Valid: true},
		BatchSize: batchSize,
	})
	if err != nil {
		return 0, fmt.Errorf("gc processed outbox rows: %w", err)
	}
	if g.deletedRows != nil {
		g.deletedRows.Add(ctx, n)
	}
	return n, nil
}
