package bench

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
)

// allowLimiter lets every eval call through; the eval measures memory
// quality, not the operation budget.
type allowLimiter struct{}

func (allowLimiter) Allow(context.Context, string) (ratelimit.Result, error) {
	return ratelimit.Result{Allowed: true, Remaining: 1, RetryAfter: 0}, nil
}

func (allowLimiter) AllowN(context.Context, string, int) (ratelimit.Result, error) {
	return ratelimit.Result{Allowed: true, Remaining: 1, RetryAfter: 0}, nil
}
