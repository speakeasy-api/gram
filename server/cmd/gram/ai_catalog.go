package gram

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed"
)

// aiCatalogStartupTimeout bounds reference bootstrap and the first read before
// either API listener can become ready.
const aiCatalogStartupTimeout = 30 * time.Second

func startAICatalog(ctx context.Context, db *pgxpool.Pool, logger *slog.Logger) (*aivendors.Store, func(), error) {
	initialCtx, initialCancel := context.WithTimeout(ctx, aiCatalogStartupTimeout)
	defer initialCancel()
	if err := catalogseed.EnsureCatalog(initialCtx, db); err != nil {
		return nil, nil, fmt.Errorf("bootstrap AI catalog: %w", err)
	}
	initial, err := aivendors.Load(initialCtx, db, logger, aitargets.ValidateCatalog)
	if err != nil {
		return nil, nil, fmt.Errorf("load initial AI catalog: %w", err)
	}
	store := aivendors.NewStore(initial)
	refreshCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); store.Run(refreshCtx, db, logger, aitargets.ValidateCatalog) }()
	return store, func() { cancel(); <-done }, nil
}
