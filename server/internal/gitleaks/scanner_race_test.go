package gitleaks_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/gitleaks"
)

// TestConcurrentScanBatchParallel verifies that multiple concurrent calls to
// ScanBatchParallel don't panic from viper's global state race. Without the
// detectorInitMu mutex, this test triggers:
//
//	fatal error: concurrent map read and map write
func TestConcurrentScanBatchParallel(t *testing.T) {
	t.Parallel()

	messages := []string{
		"normal message",
		"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7REALKEY",
		"export GITHUB_TOKEN=ghp_R2D2C3POLuk3Skywalker1234567890ab",
		"another clean message",
	}

	// Launch multiple ScanBatchParallel calls concurrently — this is what
	// happens when the Temporal workflow fans out multiple AnalyzeBatch
	// activities on the same worker.
	const concurrent = 5
	var wg sync.WaitGroup
	errs := make([]error, concurrent)

	for i := range concurrent {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := gitleaks.NewScanner().ScanBatchParallel(messages)
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "ScanBatchParallel call %d failed", i)
	}
}
