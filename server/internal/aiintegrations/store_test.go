package aiintegrations

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestInitialUsagePollWatermarkBackfillsOneHour(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 20, 12, 30, 0, 0, time.FixedZone("test", 2*60*60))

	require.Equal(t, now.UTC().Add(-time.Hour), initialUsagePollWatermark(now))
	require.Equal(t, now.UTC(), nextUsagePollAfter(initialUsagePollWatermark(now)))
}

func TestTruncateUsagePollError(t *testing.T) {
	t.Parallel()

	require.Empty(t, truncateUsagePollError(nil))
	require.Equal(t, "cursor unavailable", truncateUsagePollError(errors.New(" cursor unavailable ")))

	longErr := errors.New(strings.Repeat("x", maxUsagePollErrorMessage+1))
	require.Len(t, truncateUsagePollError(longErr), maxUsagePollErrorMessage)
}
