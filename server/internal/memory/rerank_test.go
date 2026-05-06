package memory

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestComputeScoreNoDecayWhenAgeZero(t *testing.T) {
	t.Parallel()

	got := computeScore(0.8, 0, time.Hour)
	require.InEpsilon(t, 0.8, got, 1e-9)
}

func TestComputeScoreDecaysExponentially(t *testing.T) {
	t.Parallel()

	// score = similarity * exp(-age / halfLife). At age == halfLife, the
	// decay factor is e^-1 ≈ 0.3679 (this is the formula's contract — not
	// a literal "half-life" in the radioactive sense).
	got := computeScore(1.0, time.Hour, time.Hour)
	require.InEpsilon(t, 0.3678794, got, 1e-6)

	// At 2x the half-life, decay factor is e^-2 ≈ 0.1353.
	got2x := computeScore(1.0, 2*time.Hour, time.Hour)
	require.InEpsilon(t, 0.1353353, got2x, 1e-6)

	// Score scales linearly with similarity.
	require.InEpsilon(t, 0.5*0.3678794, computeScore(0.5, time.Hour, time.Hour), 1e-6)
}

func TestComputeScoreZeroHalfLifeReturnsSimilarity(t *testing.T) {
	t.Parallel()

	got := computeScore(0.4, time.Hour, 0)
	require.InEpsilon(t, 0.4, got, 1e-9)
}

func TestSentenceBoundaryTruncateNoOpUnderLimit(t *testing.T) {
	t.Parallel()

	in := "short string"
	require.Equal(t, in, sentenceBoundaryTruncate(in, 100, "..."))
}

func TestSentenceBoundaryTruncatePrefersSentence(t *testing.T) {
	t.Parallel()

	in := "First fact. Second fact about something. Third fact runs longer."
	got := sentenceBoundaryTruncate(in, 30, "...")
	require.True(t, strings.HasSuffix(got, "..."), "expected suffix, got %q", got)
	require.Contains(t, got, "First fact.", "expected first sentence preserved")
	// "Third fact" is past the budget, so it must not appear.
	require.NotContains(t, got, "Third fact")
}

func TestSentenceBoundaryTruncateFallsBackToByteCut(t *testing.T) {
	t.Parallel()

	// No sentence terminator, must fall back to a UTF-8-safe byte cut.
	in := "abcdefghijklmnopqrstuvwxyz"
	got := sentenceBoundaryTruncate(in, 10, "…")
	require.True(t, strings.HasSuffix(got, "…"))
	require.LessOrEqual(t, len(got), 10)
}

func TestSentenceBoundaryTruncateZeroBudget(t *testing.T) {
	t.Parallel()

	require.Empty(t, sentenceBoundaryTruncate("anything", 0, "…"))
}

func TestSentenceBoundaryTruncateUTF8SafeFallback(t *testing.T) {
	t.Parallel()

	// 4-byte runes (\xF0\x9F\x98\x80 = "😀"). A naive byte cut at 5 would split a rune.
	in := "😀😀😀😀"
	got := sentenceBoundaryTruncate(in, 6, "")
	// Must end on a rune boundary.
	require.Zero(t, len(got)%4, "expected rune-aligned cut, got %d bytes", len(got))
}

func TestCapAggregateEmpty(t *testing.T) {
	t.Parallel()

	got := capAggregate(nil, 100, 100, "…")
	require.Empty(t, got)
}

func TestCapAggregateAllFit(t *testing.T) {
	t.Parallel()

	in := []RecallResult{
		{ID: uuid.New(), Content: "alpha", Score: 0.9},
		{ID: uuid.New(), Content: "beta", Score: 0.8},
	}
	got := capAggregate(in, 100, 100, "…")
	require.Len(t, got, 2)
	require.Equal(t, "alpha", got[0].Content)
	require.Equal(t, "beta", got[1].Content)
}

func TestCapAggregateDropsLowScoreTail(t *testing.T) {
	t.Parallel()

	in := []RecallResult{
		{ID: uuid.New(), Content: strings.Repeat("a", 50), Score: 0.9},
		{ID: uuid.New(), Content: strings.Repeat("b", 50), Score: 0.8},
		{ID: uuid.New(), Content: strings.Repeat("c", 50), Score: 0.7},
	}
	got := capAggregate(in, 100, 100, "…")
	require.Len(t, got, 2)
	require.Equal(t, strings.Repeat("a", 50), got[0].Content)
	require.Equal(t, strings.Repeat("b", 50), got[1].Content)
}

func TestCapAggregateTruncatesPerResult(t *testing.T) {
	t.Parallel()

	in := []RecallResult{
		{ID: uuid.New(), Content: "long. " + strings.Repeat("x", 200), Score: 0.9},
	}
	got := capAggregate(in, 20, 1000, "…")
	require.Len(t, got, 1)
	require.LessOrEqual(t, len(got[0].Content), 20)
	require.True(t, strings.HasSuffix(got[0].Content, "…"))
}

func TestSortByScoreDescStableOnTies(t *testing.T) {
	t.Parallel()

	a, b := uuid.New(), uuid.New()
	in := []RecallResult{
		{ID: a, Score: 0.5},
		{ID: b, Score: 0.5},
	}
	sortByScoreDesc(in)
	require.Equal(t, a, in[0].ID, "stable sort must preserve input order on ties")
	require.Equal(t, b, in[1].ID)
}

func TestSortByScoreDescOrders(t *testing.T) {
	t.Parallel()

	in := []RecallResult{
		{Score: 0.3},
		{Score: 0.9},
		{Score: 0.6},
	}
	sortByScoreDesc(in)
	require.InEpsilon(t, 0.9, in[0].Score, 1e-9)
	require.InEpsilon(t, 0.6, in[1].Score, 1e-9)
	require.InEpsilon(t, 0.3, in[2].Score, 1e-9)
}
