package tunnelrouting

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/speakeasy-api/gram/tunnel/route"
)

// LookupRoutes waits briefly for route publication when the initial lookup is
// empty. Its one-second budget includes store reads, not upstream requests.
// Internal expiry preserves the caller's existing no-route behavior; parent
// cancellation and store failures are returned without retrying.
func LookupRoutes(ctx context.Context, store route.Store, tunnelID string) ([]string, error) {
	lookupCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	backoff := 50 * time.Millisecond
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if lookupCtx.Err() != nil {
			return nil, nil
		}
		candidates, err := store.Candidates(lookupCtx, tunnelID)
		if parentErr := ctx.Err(); parentErr != nil {
			return nil, parentErr
		}
		if lookupCtx.Err() != nil {
			return nil, nil
		}
		if err != nil || len(candidates) > 0 {
			return candidates, err
		}

		// Jittered exponential backoff: 50–100ms, then 100–200ms.
		delay := backoff + time.Duration(rand.Int64N(int64(backoff)+1))
		timer := time.NewTimer(delay)
		select {
		case <-lookupCtx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		backoff = 100 * time.Millisecond
	}
}
