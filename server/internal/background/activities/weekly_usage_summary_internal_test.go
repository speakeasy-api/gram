package activities

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/usage"
)

func TestUsageChangePercent_Increase(t *testing.T) {
	t.Parallel()

	require.Equal(t, "+19%", usageChangePercent(1190, 1000))
}

func TestUsageChangePercent_Decrease(t *testing.T) {
	t.Parallel()

	require.Equal(t, "-92%", usageChangePercent(80, 1000))
}

func TestUsageChangePercent_Flat(t *testing.T) {
	t.Parallel()

	require.Equal(t, "+0%", usageChangePercent(1000, 1000))
}

func TestUsageChangePercent_NoPreviousUsage(t *testing.T) {
	t.Parallel()

	require.Equal(t, "New", usageChangePercent(500, 0))
	require.Equal(t, "0%", usageChangePercent(0, 0))
}

func TestUsageChangePercent_DroppedToZero(t *testing.T) {
	t.Parallel()

	require.Equal(t, "-100%", usageChangePercent(0, 1000))
}

func TestDaysUntil_RoundsPartialDaysUp(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	require.Equal(t, 8, daysUntil(now, end))
}

func TestDaysUntil_PastEndIsZero(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	require.Equal(t, 0, daysUntil(now, now))
	require.Equal(t, 0, daysUntil(now.Add(time.Hour), now))
}

func TestElapsedPercent_MidCycle(t *testing.T) {
	t.Parallel()

	cycle := usage.BillingCyclePeriod{
		Start: time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC),
	}
	// 22.5 of 31 days.
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	require.Equal(t, 73, elapsedPercent(cycle, now))
}

func TestElapsedPercent_Clamped(t *testing.T) {
	t.Parallel()

	cycle := usage.BillingCyclePeriod{
		Start: time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC),
	}
	require.Equal(t, 0, elapsedPercent(cycle, cycle.Start.Add(-time.Hour)))
	require.Equal(t, 100, elapsedPercent(cycle, cycle.End.Add(time.Hour)))
}

func TestRenderWeeklyUsageRows_PairsComponentsByKey(t *testing.T) {
	t.Parallel()

	current := []telemetryrepo.TumComponentTotal{
		{Key: "input_tokens", Label: "Input tokens", Tokens: 32030},
		{Key: "output_tokens", Label: "Output tokens", Tokens: 1190},
	}
	previous := []telemetryrepo.TumComponentTotal{
		{Key: "input_tokens", Label: "Input tokens", Tokens: 55470},
		{Key: "output_tokens", Label: "Output tokens", Tokens: 1000},
	}

	html, text, err := renderWeeklyUsageRows(current, previous, 33220, 56470)
	require.NoError(t, err)

	require.Contains(t, html, "Input tokens")
	require.Contains(t, html, "32,030")
	require.Contains(t, html, "Previous cycle at this point: 55,470")
	require.Contains(t, html, "-42%")
	require.Contains(t, html, "Output tokens")
	// html/template entity-escapes '+', which renders as "+19%" in clients.
	require.Contains(t, html, "&#43;19%")
	require.Contains(t, html, "Total")
	require.Contains(t, html, "33,220")

	require.Contains(t, text, "Input tokens: 32,030 (previous cycle at this point: 55,470, -42%)")
	require.Contains(t, text, "Total: 33,220 (previous cycle at this point: 56,470, -41%)")
}

func TestRenderWeeklyUsageRows_SortsByCurrentUsageDescending(t *testing.T) {
	t.Parallel()

	// Registry order lists input tokens first, but the email orders line
	// items by current-cycle usage, largest first.
	current := []telemetryrepo.TumComponentTotal{
		{Key: "input_tokens", Label: "Input tokens", Tokens: 100},
		{Key: "output_tokens", Label: "Output tokens", Tokens: 9000},
		{Key: "cache_write_tokens", Label: "Cache write tokens", Tokens: 500},
	}

	html, text, err := renderWeeklyUsageRows(current, nil, 9600, 0)
	require.NoError(t, err)

	lines := strings.Split(text, "\n")
	require.Len(t, lines, 4)
	require.True(t, strings.HasPrefix(lines[0], "Output tokens:"))
	require.True(t, strings.HasPrefix(lines[1], "Cache write tokens:"))
	require.True(t, strings.HasPrefix(lines[2], "Input tokens:"))
	require.True(t, strings.HasPrefix(lines[3], "Total:"))

	require.Less(t, strings.Index(html, "Output tokens"), strings.Index(html, "Cache write tokens"))
	require.Less(t, strings.Index(html, "Cache write tokens"), strings.Index(html, "Input tokens"))
}

func TestRenderWeeklyUsageRows_MissingPreviousComponentReadsAsNew(t *testing.T) {
	t.Parallel()

	// A component added to the TUM definition since the previous cycle has no
	// counterpart row; it must render as "New", not error out.
	current := []telemetryrepo.TumComponentTotal{
		{Key: "cache_read_tokens", Label: "Cache read tokens", Tokens: 4200},
	}

	html, text, err := renderWeeklyUsageRows(current, nil, 4200, 0)
	require.NoError(t, err)
	require.Contains(t, html, "Cache read tokens")
	require.Contains(t, html, "New")
	require.Contains(t, text, "Cache read tokens: 4,200 (previous cycle at this point: 0, New)")
}

func TestRenderWeeklyUsageRows_EscapesLabels(t *testing.T) {
	t.Parallel()

	current := []telemetryrepo.TumComponentTotal{
		{Key: "x", Label: "<script>alert(1)</script>", Tokens: 1},
	}

	html, _, err := renderWeeklyUsageRows(current, nil, 1, 0)
	require.NoError(t, err)
	require.NotContains(t, html, "<script>")
}
