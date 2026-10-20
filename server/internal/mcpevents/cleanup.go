package mcpevents

import (
	"context"
	"errors"
	"fmt"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpevents/repo"
	"time"
)

const (
	// subscriptionRetention allows in-flight delivery transactions to settle after expiry.
	subscriptionRetention = 24 * time.Hour

	// deliveryRetention outlasts the delivery horizon and matches broker retention.
	deliveryRetention = 7 * 24 * time.Hour

	// cleanupBatchSize bounds row locks held by each maintenance transaction.
	cleanupBatchSize = 500

	// cleanupMaxBatches bounds one table to 50,000 rows per activity attempt.
	cleanupMaxBatches = 100
)

// Cleanup is a worker-only cross-tenant sweep. It retains deduplication records
// beyond the supported delivery horizon, then permanently deletes keys and data.
func Cleanup(ctx context.Context, db repo.DBTX, now time.Time) error {
	q := repo.New(db)
	return errors.Join(
		cleanupBatches(ctx, func(ctx context.Context) (int64, error) {
			return q.DetachExpiredEventSubscriptions(ctx, repo.DetachExpiredEventSubscriptionsParams{
				Cutoff: conv.ToPGTimestamptz(now.Add(-subscriptionRetention)), BatchSize: cleanupBatchSize,
			})
		}),
		cleanupBatches(ctx, func(ctx context.Context) (int64, error) {
			return q.CleanupExpiredEventSubscriptions(ctx, repo.CleanupExpiredEventSubscriptionsParams{
				Cutoff: conv.ToPGTimestamptz(now.Add(-subscriptionRetention)), BatchSize: cleanupBatchSize,
			})
		}),
		cleanupBatches(ctx, func(ctx context.Context) (int64, error) {
			return q.CleanupOldEventDeliveries(ctx, repo.CleanupOldEventDeliveriesParams{
				Cutoff: conv.ToPGTimestamptz(now.Add(-deliveryRetention)), BatchSize: cleanupBatchSize,
			})
		}),
	)
}

func cleanupBatches(ctx context.Context, cleanup func(context.Context) (int64, error)) error {
	for range cleanupMaxBatches {
		count, err := cleanup(ctx)
		if err != nil {
			return fmt.Errorf("cleanup event records: %w", err)
		}
		if count < cleanupBatchSize {
			return nil
		}
	}
	return errors.New("event cleanup batch budget exhausted")
}
