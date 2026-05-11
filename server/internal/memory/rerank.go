package memory

import (
	"math"
	"regexp"
	"sort"
	"time"
	"unicode/utf8"
)

// sentenceBoundaryPattern matches a sentence terminator followed by whitespace.
var sentenceBoundaryPattern = regexp.MustCompile(`[.!?]\s+`)

// computeScore weights a similarity by an exponential recency decay keyed off
// the half-life. score = similarity * exp(-age / halfLife).
func computeScore(similarity float64, age, halfLife time.Duration) float64 {
	if halfLife <= 0 {
		return similarity
	}
	if age <= 0 {
		return similarity
	}
	decay := math.Exp(-age.Seconds() / halfLife.Seconds())
	return similarity * decay
}

// sentenceBoundaryTruncate clips s so that its byte length plus the suffix is
// at most maxBytes. It prefers the last sentence boundary inside the window;
// if no boundary exists it falls back to a rune-safe byte cut.
func sentenceBoundaryTruncate(s string, maxBytes int, suffix string) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}

	suffixBytes := len(suffix)
	if suffixBytes >= maxBytes {
		return suffix[:maxBytes]
	}

	budget := maxBytes - suffixBytes
	window := s
	if len(window) > budget {
		window = window[:budget]
	}

	matches := sentenceBoundaryPattern.FindAllStringIndex(window, -1)
	if len(matches) > 0 {
		last := matches[len(matches)-1]
		return s[:last[1]] + suffix
	}

	cut := budget
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + suffix
}

// capAggregate caps each result to perResult bytes via sentence-boundary
// truncation, then drops lowest-scored entries until the total content size
// fits within aggregate. Input must already be sorted by score descending.
func capAggregate(results []RecallResult, perResult, aggregate int, suffix string) []RecallResult {
	if len(results) == 0 {
		return results
	}

	truncated := make([]RecallResult, len(results))
	for i, r := range results {
		r.Content = sentenceBoundaryTruncate(r.Content, perResult, suffix)
		truncated[i] = r
	}

	if aggregate <= 0 {
		return truncated
	}

	// Prefix scan; once we exceed the aggregate budget, drop the rest. Input is
	// sorted by score desc, so dropping the tail removes the lowest-scored.
	total := 0
	for i, r := range truncated {
		total += len(r.Content)
		if total > aggregate {
			return truncated[:i]
		}
	}
	return truncated
}

// sortByScoreDesc sorts results by score descending, stable on ties.
func sortByScoreDesc(results []RecallResult) {
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
}
