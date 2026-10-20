package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

// workloadDeviceKeyLookupRate bounds device key lookups per issuer. The lookup
// runs on the unverified sub, so unbounded, every forged assertion costs a query.
var workloadDeviceKeyLookupRate = ratelimit.PerMinute(120).WithBurst(30)

// workloadDeviceKeyLookupTimeout bounds one device key lookup, budget charge
// included.
const workloadDeviceKeyLookupTimeout = 5 * time.Second

// errWorkloadDeviceKeyLookupRateLimited reports a device key lookup refused
// for budget. Nothing was decided about the assertion.
var errWorkloadDeviceKeyLookupRateLimited = errors.New("workload device key lookups are rate limited for this issuer")

// workloadDeviceKeyRateLimitedError carries the limiter's Retry-After.
type workloadDeviceKeyRateLimitedError struct {
	retryAfter time.Duration
}

func (e *workloadDeviceKeyRateLimitedError) Error() string {
	return fmt.Sprintf("%s: retry after %s", errWorkloadDeviceKeyLookupRateLimited, e.retryAfter)
}

func (e *workloadDeviceKeyRateLimitedError) Unwrap() error {
	return errWorkloadDeviceKeyLookupRateLimited
}

// newWorkloadDeviceKeyLookupBudget builds the per-issuer ceiling, or nil when
// there is no store. A nil budget refuses, as the issuer lookup's does.
func newWorkloadDeviceKeyLookupBudget(redisClient *redis.Client, meterProvider metric.MeterProvider) workloadIssuerBudget {
	if redisClient == nil {
		return nil
	}
	limiter := ratelimit.New(
		ratelimit.NewRedisStore(redisClient),
		"workload_device_key_lookup",
		workloadDeviceKeyLookupRate,
		ratelimit.WithMetrics(meterProvider),
	)
	return limiter.Allow
}

// budgetedDeviceKeyLookup charges each lookup to the resolved issuer row, which
// the caller cannot vary, so changing sub never mints a fresh budget.
func budgetedDeviceKeyLookup(lookup workloadDeviceKeyLookup, charge workloadIssuerBudget) workloadDeviceKeyLookup {
	return func(ctx context.Context, organizationID string, issuerID uuid.UUID, subject string) (workloadidentity.DeviceKey, error) {
		if charge == nil {
			return workloadidentity.DeviceKey{}, errors.New("workload device key lookup limiter unavailable")
		}
		// The sub is unverified, so a stalled store must not hold a forged
		// request open until the caller's own deadline.
		lookupCtx, cancel := context.WithTimeout(ctx, workloadDeviceKeyLookupTimeout)
		defer cancel()
		charged, err := charge(lookupCtx, "workload-device-key-lookup:"+issuerID.String())
		if err != nil {
			return workloadidentity.DeviceKey{}, fmt.Errorf("workload device key lookup limiter unavailable: %w", err)
		}
		if !charged.Allowed {
			return workloadidentity.DeviceKey{}, &workloadDeviceKeyRateLimitedError{retryAfter: charged.RetryAfter}
		}
		return lookup(lookupCtx, organizationID, issuerID, subject)
	}
}
