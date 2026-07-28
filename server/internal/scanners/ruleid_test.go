package scanners_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners"
)

func TestGuard_PanicsOnMalformedRuleIDInTest(t *testing.T) {
	t.Parallel()

	// testing.Testing() is true here so enforceRuleIDFormat is on. Wrap a
	// known-bad id and assert it panics.
	require.Panics(t, func() {
		scanners.GuardRuleID("UPPER_SNAKE_INVALID")
	})
}
