package testenv

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRetryDockerInfoRetriesTransientFailures(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		err := retryDockerInfo(t.Context(), func(context.Context) error {
			attempts++
			if attempts < 3 {
				return errors.New("docker unavailable")
			}
			return nil
		})

		require.NoError(t, err)
		require.Equal(t, 3, attempts)
	})
}

func TestRetryDockerInfoStopsAtDeadline(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		err := retryDockerInfo(ctx, func(context.Context) error {
			return errors.New("docker unavailable")
		})

		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.ErrorContains(t, err, "docker info after retries")
	})
}
