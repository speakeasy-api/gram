package gram

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

func startAICatalog(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger, meterProvider metric.MeterProvider) (*aivendors.Store, func(), error) {
	initial, err := aivendors.Load(ctx, db, logger, aitargets.ValidateCatalog)
	if err != nil {
		return nil, nil, fmt.Errorf("load initial AI catalog: %w", err)
	}
	store := aivendors.NewStore(initial)
	refreshCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); store.Run(refreshCtx, db, logger, meterProvider, aitargets.ValidateCatalog) }()
	return store, func() { cancel(); <-done }, nil
}
