package search

// RRFResult holds an item ID and its combined RRF score.
type RRFResult struct {
	ID    string
	Score float64
}

// RRF computes Reciprocal Rank Fusion across multiple ranked lists.
// Each list is a slice of item IDs ordered by descending relevance.
// weights[i] is the weight for lists[i]. K is the RRF constant (typically 60).
// score = Σ weight[i] / (K + rank), where rank is 1-based.
func RRF(lists [][]string, weights []float64, k float64) []RRFResult {
	panic("not implemented")
}
