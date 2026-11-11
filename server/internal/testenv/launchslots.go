// TEMP: cache-busting comment for CI timing comparison (run 3).
package testenv

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	// maxConcurrentLaunchesEnv names the variable that bounds how many
	// Postgres, Redis, and ClickHouse test containers may be starting at the
	// same time across every test binary on one machine. Unset or 0 means
	// unbounded. CI sets it so that 'go test' can build and link with full
	// parallelism while container startup (schema init and server boot, the
	// CPU-heavy part of a package's TestMain) stays bounded.
	maxConcurrentLaunchesEnv = "GRAM_TESTENV_MAX_CONCURRENT_LAUNCHES"

	// launchSlotPollInterval is how often a waiting test binary retries the
	// slot locks. Container startup takes seconds, so a short poll adds no
	// meaningful delay.
	launchSlotPollInterval = 100 * time.Millisecond
)

// acquireLaunchSlot blocks until the caller holds one of the machine-wide
// container launch slots, and returns a function that releases it. Callers
// hold the slot until the container is ready, not for its lifetime. Slots are
// lock files shared by every test binary on the machine; the operating system
// releases a lock when its holder exits, so a crashed binary cannot leak one.
func acquireLaunchSlot(ctx context.Context) (func(), error) {
	limit, err := maxConcurrentLaunches()
	if err != nil {
		return nil, err
	}
	if limit == 0 {
		return func() {}, nil
	}

	return acquireLaunchSlotIn(ctx, filepath.Join(os.TempDir(), "gram-testenv-launch-slots"), limit)
}

// acquireLaunchSlotIn takes one of limit slot locks in dir.
func acquireLaunchSlotIn(ctx context.Context, dir string, limit int) (func(), error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create launch slot directory: %w", err)
	}

	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for container launch slot: %w", ctx.Err())
		case <-timer.C:
		}

		for i := range limit {
			release, ok, err := tryLockFile(filepath.Join(dir, fmt.Sprintf("slot-%d.lock", i)))
			if err != nil {
				return nil, fmt.Errorf("lock container launch slot: %w", err)
			}
			if ok {
				return release, nil
			}
		}

		timer.Reset(launchSlotPollInterval)
	}
}

func maxConcurrentLaunches() (int, error) {
	raw := os.Getenv(maxConcurrentLaunchesEnv)
	if raw == "" {
		return 0, nil
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer, got %q", maxConcurrentLaunchesEnv, raw)
	}

	return n, nil
}
