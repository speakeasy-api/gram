package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
)

func TestBudgetedDeviceKeyLookup(t *testing.T) {
	t.Parallel()

	issuerID := uuid.New()
	found := workloadidentity.DeviceKey{Thumbprint: "kid"}

	newLookup := func(calls *int) workloadDeviceKeyLookup {
		return func(context.Context, string, uuid.UUID, string) (workloadidentity.DeviceKey, error) {
			*calls++
			return found, nil
		}
	}

	t.Run("charges the issuer's budget, never the subject's", func(t *testing.T) {
		t.Parallel()
		var calls int
		var scopes []string
		charge := func(_ context.Context, scope string) (ratelimit.Result, error) {
			scopes = append(scopes, scope)
			return ratelimit.Result{Allowed: true, Remaining: 1, RetryAfter: 0}, nil
		}
		lookup := budgetedDeviceKeyLookup(newLookup(&calls), charge)

		for _, subject := range []string{"device:a", "device:b"} {
			key, err := lookup(t.Context(), "org", issuerID, subject)
			require.NoError(t, err)
			require.Equal(t, found, key)
		}
		require.Equal(t, 2, calls)
		require.Equal(t, []string{"workload-device-key-lookup:" + issuerID.String(), "workload-device-key-lookup:" + issuerID.String()}, scopes)
	})

	t.Run("the charge and the lookup are bounded even when the caller is not", func(t *testing.T) {
		t.Parallel()
		within := func(ctx context.Context) {
			deadline, ok := ctx.Deadline()
			require.True(t, ok, "the stage must carry its own deadline")
			require.LessOrEqual(t, time.Until(deadline), workloadDeviceKeyLookupTimeout)
		}
		charge := func(ctx context.Context, _ string) (ratelimit.Result, error) {
			within(ctx)
			return ratelimit.Result{Allowed: true, Remaining: 1, RetryAfter: 0}, nil
		}
		var calls int
		lookup := func(ctx context.Context, _ string, _ uuid.UUID, _ string) (workloadidentity.DeviceKey, error) {
			calls++
			within(ctx)
			return found, nil
		}
		_, err := budgetedDeviceKeyLookup(lookup, charge)(context.Background(), "org", issuerID, "device:a")
		require.NoError(t, err)
		require.Equal(t, 1, calls)
	})

	t.Run("a refused charge never reaches the store", func(t *testing.T) {
		t.Parallel()
		var calls int
		charge := func(context.Context, string) (ratelimit.Result, error) {
			return ratelimit.Result{Allowed: false, Remaining: 0, RetryAfter: 7 * time.Second}, nil
		}
		_, err := budgetedDeviceKeyLookup(newLookup(&calls), charge)(t.Context(), "org", issuerID, "device:a")
		require.ErrorIs(t, err, errWorkloadDeviceKeyLookupRateLimited)
		limited, ok := errors.AsType[*workloadDeviceKeyRateLimitedError](err)
		require.True(t, ok)
		require.Equal(t, 7*time.Second, limited.retryAfter)
		require.Zero(t, calls)
	})

	t.Run("an unavailable limiter fails closed", func(t *testing.T) {
		t.Parallel()
		var calls int
		charge := func(context.Context, string) (ratelimit.Result, error) {
			return ratelimit.Result{Allowed: false, Remaining: 0, RetryAfter: 0}, errors.New("store down")
		}
		_, err := budgetedDeviceKeyLookup(newLookup(&calls), charge)(t.Context(), "org", issuerID, "device:a")
		require.Error(t, err)
		require.NotErrorIs(t, err, errWorkloadDeviceKeyLookupRateLimited)
		require.Zero(t, calls)

		_, err = budgetedDeviceKeyLookup(newLookup(&calls), nil)(t.Context(), "org", issuerID, "device:a")
		require.Error(t, err)
		require.Zero(t, calls)
	})
}
