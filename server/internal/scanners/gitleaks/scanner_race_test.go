package gitleaks_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners/gitleaks"
)

// TestConcurrentScan runs many Scan calls at once across fresh scanners. Every
// detector shares one memoized config, so under -race this checks that
// detection never writes to it, and that the first, concurrent build of that
// config does not race on gitleaks' global viper state.
func TestConcurrentScan(t *testing.T) {
	t.Parallel()

	messages := []struct {
		content  string
		findings bool
	}{
		{content: "normal message", findings: false},
		// The access key id alone only anchors the composite rule (SkipReport).
		{content: "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7REALKEY", findings: false},
		{content: "AccessKeyId: " + fakeAccessKeyID + ", SecretAccessKey: " + fakeSecret, findings: true},
		{content: "export GITHUB_TOKEN=ghp_R2D2C3POLuk3Skywalker1234567890ab", findings: true},
		{content: "another clean message", findings: false},
		// A placeholder value is dropped by a shared allowlist stopword.
		{content: "api_key = \"your_api_key_here_placeholder\"", findings: false},
	}

	// Fan out many Scan calls concurrently across fresh scanners — this
	// approximates the Temporal workflow fanning out multiple AnalyzeBatch
	// activities on the same worker, each scanning its batch in parallel.
	const concurrent = 20
	var wg sync.WaitGroup
	errs := make([]error, concurrent)
	found := make([]bool, concurrent)

	for i := range concurrent {
		wg.Go(func() {
			scanner := gitleaks.NewScanner()
			for range 3 {
				result, err := scanner.Scan(t.Context(), messages[i%len(messages)].content)
				if err != nil {
					errs[i] = err
					return
				}
				found[i] = len(result.Findings) > 0
			}
		})
	}

	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "Scan call %d failed", i)
		require.Equal(t, messages[i%len(messages)].findings, found[i], "Scan call %d findings", i)
	}
}
