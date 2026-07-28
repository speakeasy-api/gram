package gitleaks_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
)

// TestConcurrentScan verifies that many concurrent Scan calls don't panic from
// viper's global state race during detector creation. Detectors are built lazily
// by the pool's New func, which can fire concurrently on pool misses. Without
// the detectorInitMu mutex, this test triggers:
//
//	fatal error: concurrent map read and map write
func TestConcurrentScan(t *testing.T) {
	t.Parallel()

	messages := []string{
		"normal message",
		"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7REALKEY",
		"export GITHUB_TOKEN=ghp_R2D2C3POLuk3Skywalker1234567890ab",
		"another clean message",
	}

	// Fan out many Scan calls concurrently across fresh scanners — this
	// approximates the Temporal workflow fanning out multiple AnalyzeBatch
	// activities on the same worker, each scanning its batch in parallel.
	const concurrent = 20
	var wg sync.WaitGroup
	errs := make([]error, concurrent)

	for i := range concurrent {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := gitleaks.NewScanner().Scan(t.Context(), messages[idx%len(messages)])
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "Scan call %d failed", i)
	}
}
