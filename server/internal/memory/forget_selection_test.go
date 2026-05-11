package memory

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/memory/repo"
)

func TestDecideForgetSelectionEmptyIsNoMatch(t *testing.T) {
	t.Parallel()

	got := decideForgetSelection(nil, 0.65, 0.05)
	require.Equal(t, forgetOutcomeNoMatch, got.Outcome)
	require.Empty(t, got.Candidates)
}

func TestDecideForgetSelectionLowSimilarityIsNoMatch(t *testing.T) {
	t.Parallel()

	rows := []repo.ListNearestAssistantMemoriesRow{
		{ID: uuid.New(), Similarity: 0.40},
	}
	got := decideForgetSelection(rows, 0.65, 0.05)
	require.Equal(t, forgetOutcomeNoMatch, got.Outcome)
}

func TestDecideForgetSelectionAmbiguousWhenGapTooSmall(t *testing.T) {
	t.Parallel()

	rows := []repo.ListNearestAssistantMemoriesRow{
		{ID: uuid.New(), Content: "first", Similarity: 0.85},
		{ID: uuid.New(), Content: "second", Similarity: 0.83},
		{ID: uuid.New(), Content: "third", Similarity: 0.70},
	}
	got := decideForgetSelection(rows, 0.65, 0.05)
	require.Equal(t, forgetOutcomeAmbiguous, got.Outcome)
	require.Len(t, got.Candidates, 3)
	require.Equal(t, "first", got.Candidates[0].Content)
}

func TestDecideForgetSelectionHitWhenGapClear(t *testing.T) {
	t.Parallel()

	rows := []repo.ListNearestAssistantMemoriesRow{
		{ID: uuid.New(), Content: "first", Similarity: 0.95},
		{ID: uuid.New(), Content: "second", Similarity: 0.70},
	}
	got := decideForgetSelection(rows, 0.65, 0.05)
	require.Equal(t, forgetOutcomeHit, got.Outcome)
}

func TestDecideForgetSelectionHitOnSingleAboveThreshold(t *testing.T) {
	t.Parallel()

	rows := []repo.ListNearestAssistantMemoriesRow{
		{ID: uuid.New(), Content: "only", Similarity: 0.80},
	}
	got := decideForgetSelection(rows, 0.65, 0.05)
	require.Equal(t, forgetOutcomeHit, got.Outcome)
}

func TestDecideForgetSelectionExactBoundary(t *testing.T) {
	t.Parallel()

	// At exactly the min similarity, treat as match (>= not >).
	rows := []repo.ListNearestAssistantMemoriesRow{
		{ID: uuid.New(), Content: "boundary", Similarity: 0.65},
	}
	got := decideForgetSelection(rows, 0.65, 0.05)
	require.Equal(t, forgetOutcomeHit, got.Outcome)
}

func TestDecideForgetSelectionExactGap(t *testing.T) {
	t.Parallel()

	// At exactly the ambiguity gap, treat as hit (gap < ambiguityGap, not <=).
	rows := []repo.ListNearestAssistantMemoriesRow{
		{ID: uuid.New(), Similarity: 0.90},
		{ID: uuid.New(), Similarity: 0.85},
	}
	got := decideForgetSelection(rows, 0.65, 0.05)
	require.Equal(t, forgetOutcomeHit, got.Outcome)
}
