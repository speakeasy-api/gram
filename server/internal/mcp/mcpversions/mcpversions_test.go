package mcpversions_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
)

func TestAllIsChronologicallyOrdered(t *testing.T) {
	t.Parallel()

	versions := mcpversions.All()
	require.True(t, slices.IsSorted(versions), "revision identifiers are YYYY-MM-DD, so chronological order is lexical order")
}

func TestAllHasNoDuplicates(t *testing.T) {
	t.Parallel()

	versions := mcpversions.All()
	deduped := slices.Compact(slices.Clone(versions))
	require.Equal(t, versions, deduped)
}

func TestAllReturnsACopy(t *testing.T) {
	t.Parallel()

	first := mcpversions.All()
	first[0] = "mutated"

	require.Equal(t, mcpversions.Version20241105, mcpversions.All()[0], "All must not hand out a mutable view of package state")
}

// TestServedVersionsAreKnown guards every surface's served revision against
// drifting out of the registry. Add a case here when a surface is added.
func TestServedVersionsAreKnown(t *testing.T) {
	t.Parallel()

	require.True(t, mcpversions.Known(mcpversions.ServedHostedToolset))
	require.True(t, mcpversions.Known(mcpversions.ServedPlatformToolset))
}

func TestKnownAcceptsEveryPublishedRevision(t *testing.T) {
	t.Parallel()

	for _, v := range mcpversions.All() {
		require.True(t, mcpversions.Known(v), "expected %q to be recognized", v)
	}
}

func TestKnownRejectsUnrecognizedValues(t *testing.T) {
	t.Parallel()

	require.False(t, mcpversions.Known(""))
	require.False(t, mcpversions.Known("2025-03-27"))
	require.False(t, mcpversions.Known("garbage"))
	require.False(t, mcpversions.Known(" 2025-03-26"), "Known matches exactly; callers sanitize first")
}

func TestClampPassesThroughKnownVersions(t *testing.T) {
	t.Parallel()

	for _, v := range mcpversions.All() {
		require.Equal(t, v, mcpversions.Clamp(v))
	}
}

func TestClampBucketsUnknownVersions(t *testing.T) {
	t.Parallel()

	require.Equal(t, mcpversions.Other, mcpversions.Clamp("2999-01-01"))
	require.Equal(t, mcpversions.Other, mcpversions.Clamp("not-a-version"))
}

func TestClampBucketsAnAbsentVersionAsNone(t *testing.T) {
	t.Parallel()

	// "the client said nothing" and "the client said something unrecognized"
	// are different facts, so they get different buckets — but both get one,
	// so a breakdown by version accounts for every point.
	require.Equal(t, mcpversions.None, mcpversions.Clamp(""))
}

func TestClampNeverReturnsEmpty(t *testing.T) {
	t.Parallel()

	// Metric callers record the dimension unconditionally and rely on this.
	for _, v := range []string{"", " ", "garbage", "2999-01-01", mcpversions.Version20250618} {
		require.NotEmpty(t, mcpversions.Clamp(v), "input %q", v)
	}
}

// TestClampDoesNotLetClientsForgeSyntheticBuckets pins that the two synthetic
// bucket names are not reachable by a client sending them literally.
func TestClampDoesNotLetClientsForgeSyntheticBuckets(t *testing.T) {
	t.Parallel()

	require.Equal(t, mcpversions.Other, mcpversions.Clamp(mcpversions.None))
	require.Equal(t, mcpversions.Other, mcpversions.Clamp(mcpversions.Other))
}

func TestSanitizeTrimsSurroundingWhitespace(t *testing.T) {
	t.Parallel()

	require.Equal(t, mcpversions.Version20250618, mcpversions.Sanitize("  2025-06-18\t"))
}

func TestSanitizeCapsLength(t *testing.T) {
	t.Parallel()

	got := mcpversions.Sanitize(strings.Repeat("a", 500))
	require.Len(t, got, 32)
}

func TestSanitizeRejectsNonPrintableASCII(t *testing.T) {
	t.Parallel()

	require.Empty(t, mcpversions.Sanitize("2025-06-18\x00"))
	require.Empty(t, mcpversions.Sanitize("2025\n06-18"))
	require.Empty(t, mcpversions.Sanitize("2025-06-18é"))
}

func TestSanitizeTrimsRatherThanRejectsSurroundingControlBytes(t *testing.T) {
	t.Parallel()

	// Trimming runs before the printable-ASCII check, so a header value with
	// stray leading/trailing whitespace is cleaned up rather than discarded.
	require.Equal(t, mcpversions.Version20250618, mcpversions.Sanitize("2025-06-18\n"))
}

func TestSanitizePreservesUnknownButWellFormedValues(t *testing.T) {
	t.Parallel()

	// The raw value is the diagnostic payload — Sanitize bounds it, it does
	// not restrict it to known revisions the way Clamp does.
	require.Equal(t, "1999-12-31", mcpversions.Sanitize("1999-12-31"))
}
