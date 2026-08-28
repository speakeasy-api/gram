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

// TestSupportedSetsAreKnownAndOrdered guards every surface's supported set
// against drifting out of the registry or out of the oldest-first ordering
// Negotiate's highest-supported fallback depends on. Add a case here when a
// surface is added.
func TestSupportedSetsAreKnownAndOrdered(t *testing.T) {
	t.Parallel()

	for _, supported := range [][]string{
		mcpversions.SupportedHostedToolset(),
		mcpversions.SupportedPlatformToolset(),
		mcpversions.SupportedMetaServer(),
	} {
		require.NotEmpty(t, supported)
		require.True(t, slices.IsSorted(supported), "revision identifiers are YYYY-MM-DD, so chronological order is lexical order")
		require.Contains(t, supported, mcpversions.DefaultInEffect, "the unversioned default must itself be servable")
		for _, v := range supported {
			require.True(t, mcpversions.Known(v), "expected %q to be recognized", v)
		}
	}
}

func TestSupportedSetsReturnCopies(t *testing.T) {
	t.Parallel()

	mcpversions.SupportedHostedToolset()[0] = "mutated"
	mcpversions.SupportedPlatformToolset()[0] = "mutated"

	require.Equal(t, mcpversions.Version20241105, mcpversions.SupportedHostedToolset()[0], "SupportedHostedToolset must not hand out a mutable view of package state")
	require.Equal(t, mcpversions.Version20241105, mcpversions.SupportedPlatformToolset()[0], "SupportedPlatformToolset must not hand out a mutable view of package state")
}

// TestSupportedSetsExclude20260728 pins the current ceiling: advertising
// 2026-07-28 is its own project, and adding it to a set is the entire
// behavioral switch for that work — it must not happen by accident.
func TestSupportedSetsExclude20260728(t *testing.T) {
	t.Parallel()

	require.NotContains(t, mcpversions.SupportedHostedToolset(), mcpversions.Version20260728)
	require.NotContains(t, mcpversions.SupportedPlatformToolset(), mcpversions.Version20260728)
}

func TestNegotiateEchoesEverySupportedVersion(t *testing.T) {
	t.Parallel()

	supported := mcpversions.SupportedHostedToolset()
	for _, v := range supported {
		require.Equal(t, v, mcpversions.Negotiate(v, supported))
	}
}

// TestNegotiateAnswersAbsentWithTheDefault pins that the no-version cohort is
// not handed the ceiling: a client that omitted the field entirely is the
// likeliest to break on a newer revision, and the spec's omitted-version rule
// points at 2025-03-26.
func TestNegotiateAnswersAbsentWithTheDefault(t *testing.T) {
	t.Parallel()

	require.Equal(t, mcpversions.DefaultInEffect, mcpversions.Negotiate("", mcpversions.SupportedHostedToolset()))
}

func TestNegotiateAnswersUnsupportedWithTheNewestSupported(t *testing.T) {
	t.Parallel()

	supported := mcpversions.SupportedHostedToolset()

	// The expected value is pinned rather than derived from the set, so
	// raising the ceiling breaks this test and forces choosing new out-of-set
	// inputs that keep the fallback arm exercised.
	require.Equal(t, mcpversions.Version20251125, mcpversions.Negotiate(mcpversions.Version20260728, supported), "known but unsupported")
	require.Equal(t, mcpversions.Version20251125, mcpversions.Negotiate("1999-12-31", supported), "well-formed but unrecognized")
	require.Equal(t, mcpversions.Version20251125, mcpversions.Negotiate("garbage", supported), "not a version at all")
}

func TestNegotiateSanitizesRawClientInput(t *testing.T) {
	t.Parallel()

	supported := mcpversions.SupportedHostedToolset()

	require.Equal(t, mcpversions.Version20250618, mcpversions.Negotiate("  2025-06-18\t", supported), "surrounding whitespace trims to a supported version")
	require.Equal(t, mcpversions.DefaultInEffect, mcpversions.Negotiate("2025-06-18\x00", supported), "non-printable input sanitizes to absent, not to the ceiling")
}

func TestResolveKeepsEverySupportedDeclarationInEffect(t *testing.T) {
	t.Parallel()

	supported := mcpversions.SupportedHostedToolset()
	for _, v := range supported {
		got := mcpversions.Resolve(v, supported)
		require.Equal(t, v, got.Declared)
		require.Equal(t, v, got.InEffect)
	}
}

func TestResolveDefaultsAnAbsentDeclaration(t *testing.T) {
	t.Parallel()

	got := mcpversions.Resolve("", mcpversions.SupportedHostedToolset())
	require.Empty(t, got.Declared, "telemetry must still see that nothing was declared")
	require.Equal(t, mcpversions.DefaultInEffect, got.InEffect)
}

// TestResolveDefaultsAnUnsupportedDeclaration pins the over-serve-downward
// arm: a declaration outside the supported set keeps its raw value for
// telemetry while behavior falls back to the default rather than trusting it.
func TestResolveDefaultsAnUnsupportedDeclaration(t *testing.T) {
	t.Parallel()

	got := mcpversions.Resolve(mcpversions.Version20260728, mcpversions.SupportedHostedToolset())
	require.Equal(t, mcpversions.Version20260728, got.Declared)
	require.Equal(t, mcpversions.DefaultInEffect, got.InEffect)
}

func TestResolveSanitizesRawClientInput(t *testing.T) {
	t.Parallel()

	got := mcpversions.Resolve("  2025-06-18\t", mcpversions.SupportedHostedToolset())
	require.Equal(t, mcpversions.Version20250618, got.Declared)
	require.Equal(t, mcpversions.Version20250618, got.InEffect)
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

// TestSupportedMetaServerSpansFloorToCeiling pins the meta surface's range:
// the hosted floor (mainstream clients are the same installed base) up
// through 2026-07-28, which this surface has served since birth.
func TestSupportedMetaServerSpansFloorToCeiling(t *testing.T) {
	t.Parallel()

	set := mcpversions.SupportedMetaServer()
	require.Equal(t, mcpversions.Version20241105, set[0])
	require.Equal(t, mcpversions.Version20260728, set[len(set)-1])

	mcpversions.SupportedMetaServer()[0] = "mutated"
	require.Equal(t, mcpversions.Version20241105, mcpversions.SupportedMetaServer()[0], "SupportedMetaServer must not hand out a mutable view of package state")
}
