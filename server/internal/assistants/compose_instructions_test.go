package assistants

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestComposeInstructionsInjectsCurrentDate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 6, 14, 30, 0, 0, time.UTC)
	thread := assistantThreadRecord{
		ID:            uuid.New(),
		AssistantID:   uuid.New(),
		ProjectID:     uuid.New(),
		CorrelationID: "c1",
		ChatID:        uuid.New(),
		SourceKind:    sourceKindDashboard,
		SourceRefJSON: []byte(`{}`),
		LastEventAt:   now,
	}

	out, err := composeInstructions("You are a helpful assistant.", thread, now)
	require.NoError(t, err)
	require.Contains(t, out, "The current date is 2026-06-06.")
	require.Contains(t, out, "You are a helpful assistant.")
}

// The date is taken from the supplied clock, not wall-clock, and is rendered in
// UTC regardless of the location of the passed-in time.
func TestComposeInstructionsUsesSuppliedClockInUTC(t *testing.T) {
	t.Parallel()

	// 2026-06-06T23:30 in a -03:00 zone is 2026-06-07T02:30 in UTC.
	loc := time.FixedZone("test", -3*60*60)
	now := time.Date(2026, time.June, 6, 23, 30, 0, 0, loc)
	thread := assistantThreadRecord{
		SourceKind:    sourceKindDashboard,
		SourceRefJSON: []byte(`{}`),
	}

	out, err := composeInstructions("base", thread, now)
	require.NoError(t, err)
	require.Contains(t, out, "The current date is 2026-06-07.")
	require.NotContains(t, out, "2026-06-06")
}

// An empty base prompt still gets the date line — the date isn't conditional on
// the static instructions being present.
func TestComposeInstructionsDateWithEmptyBase(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 6, 0, 0, 0, 0, time.UTC)
	thread := assistantThreadRecord{
		SourceKind:    sourceKindDashboard,
		SourceRefJSON: []byte(`{}`),
	}

	out, err := composeInstructions("", thread, now)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(out, "The current date is 2026-06-06."),
		"date line should lead when base is empty, got: %q", out)
}
