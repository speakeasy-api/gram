package search

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRRF_CombinesRankedLists(t *testing.T) {
	t.Parallel()

	// Three ranked lists with equal weights.
	lists := [][]string{
		{"a", "b", "c"},
		{"b", "c", "a"},
		{"c", "a", "b"},
	}
	weights := []float64{1.0, 1.0, 1.0}
	k := 60.0

	results := RRF(lists, weights, k)

	// Each item appears at ranks 1, 2, 3 across the three lists (in some order).
	// For each item: score = 1/(60+1) + 1/(60+2) + 1/(60+3) = 1/61 + 1/62 + 1/63
	// All items should have the same score since each appears once at each rank.
	expectedScore := 1.0/61.0 + 1.0/62.0 + 1.0/63.0

	require.Len(t, results, 3)
	for _, r := range results {
		require.InDelta(t, expectedScore, r.Score, 1e-10, "item %s should have expected RRF score", r.ID)
	}
}

func TestRRF_HandlesDisjointLists(t *testing.T) {
	t.Parallel()

	// Items appear in only some lists.
	lists := [][]string{
		{"a", "b"},      // a=rank1, b=rank2
		{"c", "a"},      // c=rank1, a=rank2
		{"b", "c", "d"}, // b=rank1, c=rank2, d=rank3
	}
	weights := []float64{1.0, 1.0, 1.0}
	k := 60.0

	results := RRF(lists, weights, k)

	scores := make(map[string]float64, len(results))
	for _, r := range results {
		scores[r.ID] = r.Score
	}

	// a: list0 rank1 + list1 rank2 = 1/61 + 1/62
	require.InDelta(t, 1.0/61.0+1.0/62.0, scores["a"], 1e-10)

	// b: list0 rank2 + list2 rank1 = 1/62 + 1/61
	require.InDelta(t, 1.0/62.0+1.0/61.0, scores["b"], 1e-10)

	// c: list1 rank1 + list2 rank2 = 1/61 + 1/62
	require.InDelta(t, 1.0/61.0+1.0/62.0, scores["c"], 1e-10)

	// d: list2 rank3 = 1/63
	require.InDelta(t, 1.0/63.0, scores["d"], 1e-10)

	// Results should be sorted descending by score.
	for i := 1; i < len(results); i++ {
		require.GreaterOrEqual(t, results[i-1].Score, results[i].Score,
			"results should be sorted descending by score")
	}

	require.Len(t, results, 4)
}
