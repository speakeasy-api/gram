package logs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A tool can be dispatched with no request body — decodeToolInput must treat a
// nil payload as empty input rather than panicking on io.ReadAll(nil).
func TestDecodeToolInputHandlesNilPayload(t *testing.T) {
	t.Parallel()

	var dst struct {
		A string `json:"a"`
	}
	require.NoError(t, decodeToolInput(nil, &dst))
	require.Empty(t, dst.A)
}

// A past `to` with an omitted `from` must yield a forward 7-day window ending at
// `to` — not `now-7d`, which would put `from` after `to` (a backward window that
// the telemetry layer rejects). Regression for the cubic finding on #3218.
func TestDefaultTimeWindowAnchorsDefaultFromToProvidedEnd(t *testing.T) {
	t.Parallel()

	to := "2026-01-10T00:00:00Z"
	gotFrom, gotTo := defaultTimeWindow("", to)

	require.Equal(t, to, gotTo)
	require.Equal(t, "2026-01-03T00:00:00Z", gotFrom)

	fromT, err := time.Parse(time.RFC3339, gotFrom)
	require.NoError(t, err)
	toT, err := time.Parse(time.RFC3339, gotTo)
	require.NoError(t, err)
	require.True(t, fromT.Before(toT), "defaulted window must run forward (from before to)")
}

// Explicit values pass through untouched.
func TestDefaultTimeWindowPreservesExplicitRange(t *testing.T) {
	t.Parallel()

	gotFrom, gotTo := defaultTimeWindow("2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z")

	require.Equal(t, "2026-01-01T00:00:00Z", gotFrom)
	require.Equal(t, "2026-01-02T00:00:00Z", gotTo)
}
