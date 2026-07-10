package activities

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

const (
	// WorkOS documents a general limit of 6,000 requests per 60 seconds per
	// source IP. Keep headroom for WorkOS calls that have not yet been moved
	// behind this limiter, and smooth webhook-driven bursts across worker pods.
	workosAPIRateLimiterName = "workos-api"
	workosAPIRateLimitKey    = "global"
	workosAPIRequestsPerMin  = 5_400
	workosAPIRequestBurst    = 20
)

type allowLimiter interface {
	Allow(ctx context.Context, key string) (ratelimit.Result, error)
}

type redisWorkOSRateLimiter struct {
	limiter allowLimiter
}

// NewWorkOSRateLimiter creates the shared WorkOS API limiter. Every caller
// uses the same key because WorkOS applies its general quota across requests,
// not per organization or user.
func NewWorkOSRateLimiter(store ratelimit.Store) WorkOSRateLimiter {
	return &redisWorkOSRateLimiter{
		limiter: ratelimit.New(
			store,
			workosAPIRateLimiterName,
			ratelimit.PerMinute(workosAPIRequestsPerMin).WithBurst(workosAPIRequestBurst),
		),
	}
}

// Wait blocks until this caller owns a token or the context is canceled.
func (l *redisWorkOSRateLimiter) Wait(ctx context.Context) error {
	for {
		res, err := l.limiter.Allow(ctx, workosAPIRateLimitKey)
		if err != nil {
			return fmt.Errorf("check workos API rate limit: %w", err)
		}
		if res.Allowed {
			return nil
		}
		if res.RetryAfter <= 0 {
			return fmt.Errorf("workos API rate limit denied without a retry delay")
		}

		timer := time.NewTimer(res.RetryAfter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("wait for workos API rate limit: %w", ctx.Err())
		case <-timer.C:
		}
	}
}
