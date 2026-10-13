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

// workloadDeviceKeyLookupRate bounds how many device key lookups one device
// issuer drives into the database. The lookup runs before the assertion is
// verified, keyed on its unverified sub, so without a bound every forged
// assertion would cost a query.
var workloadDeviceKeyLookupRate = ratelimit.PerMinute(120).WithBurst(30)

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

// budgetedDeviceKeyLookup charges every lookup against the device issuer's
// budget before it reaches the store. The scope is the issuer row the iss
// resolved to, never anything the caller controls, so varying sub cannot mint
// a fresh budget.
func budgetedDeviceKeyLookup(lookup workloadDeviceKeyLookup, charge workloadIssuerBudget) workloadDeviceKeyLookup {
	return func(ctx context.Context, organizationID string, issuerID uuid.UUID, subject string) (workloadidentity.DeviceKey, error) {
		if charge == nil {
			return workloadidentity.DeviceKey{}, errors.New("workload device key lookup limiter unavailable")
		}
		charged, err := charge(ctx, "workload-device-key-lookup:"+issuerID.String())
		if err != nil {
			return workloadidentity.DeviceKey{}, fmt.Errorf("workload device key lookup limiter unavailable: %w", err)
		}
		if !charged.Allowed {
			return workloadidentity.DeviceKey{}, &workloadDeviceKeyRateLimitedError{retryAfter: charged.RetryAfter}
		}
		return lookup(ctx, organizationID, issuerID, subject)
	}
}
