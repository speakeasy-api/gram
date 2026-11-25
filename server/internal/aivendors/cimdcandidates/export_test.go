package cimdcandidates

import (
	"context"
	"log/slog"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

// BackfillWithLimits runs the backfill with small pages and held-row bound.
func BackfillWithLimits(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, catalog *aivendors.Snapshot, assistantOrigin *url.URL, issuerPage, clientPage int32, maxHeld int) (BackfillCounts, error) {
	return backfill(ctx, logger, db, catalog, assistantOrigin, issuerPage, clientPage, maxHeld)
}
