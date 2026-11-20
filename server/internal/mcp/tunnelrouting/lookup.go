package tunnelrouting

import (
	"context"
	"errors"
	"fmt"
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
			return nil, fmt.Errorf("lookup tunnel routes: %w", err)
		}
		if lookupCtx.Err() != nil {
			return nil, nil
		}
		candidates, err := store.Candidates(lookupCtx, tunnelID)
		if parentErr := ctx.Err(); parentErr != nil {
			return nil, fmt.Errorf("lookup tunnel routes: %w", parentErr)
		}
		if err != nil {
			if lookupCtx.Err() != nil && errors.Is(err, lookupCtx.Err()) {
				return nil, nil
			}
			return nil, fmt.Errorf("list tunnel routes: %w", err)
		}
		if lookupCtx.Err() != nil {
			return nil, nil
		}
		if len(candidates) > 0 {
			return candidates, nil
		}

		// Jittered exponential backoff: 50–100ms, then 100–200ms.
		delay := backoff + time.Duration(rand.Int64N(int64(backoff)+1)) // #nosec G404 -- retry jitter is not security-sensitive
		timer := time.NewTimer(delay)
		select {
		case <-lookupCtx.Done():
			timer.Stop()
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("wait for tunnel routes: %w", err)
			}
			return nil, nil
		case <-timer.C:
		}
		backoff = 100 * time.Millisecond
	}
}
