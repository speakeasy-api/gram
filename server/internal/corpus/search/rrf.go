package search

import "slices"

// RRFResult holds an item ID and its combined RRF score.
type RRFResult struct {
	ID    string
	Score float64
}

// RRF computes Reciprocal Rank Fusion across multiple ranked lists.
// Each list is a slice of item IDs ordered by descending relevance.
// weights[i] is the weight for lists[i]. K is the RRF constant (typically 60).
// score = Σ weight[i] / (K + rank), where rank is 1-based.
// Results are returned sorted descending by score.
func RRF(lists [][]string, weights []float64, k float64) []RRFResult {
	scores := make(map[string]float64)
	for i, list := range lists {
		w := weights[i]
		for rank, id := range list {
			scores[id] += w / (k + float64(rank+1))
		}
	}

	results := make([]RRFResult, 0, len(scores))
	for id, score := range scores {
		results = append(results, RRFResult{ID: id, Score: score})
	}

	slices.SortFunc(results, func(a, b RRFResult) int {
		if a.Score > b.Score {
			return -1
		}
		if a.Score < b.Score {
			return 1
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})

	return results
}
