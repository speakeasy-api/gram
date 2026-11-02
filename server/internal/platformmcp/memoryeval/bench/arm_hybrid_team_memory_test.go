package bench

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp"
)

func hit(kind, id string) platformmcp.TeamMemoryHit {
	return platformmcp.TeamMemoryHit{Source: platformmcp.TeamMemorySource{Kind: kind, ID: id, ChatID: ""}, Text: kind + ":" + id, Truncated: false, At: time.Time{}, AuthorUserID: "", Level: "", PeriodStart: ""}
}

func ids(hits []platformmcp.TeamMemoryHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Source.Kind+":"+h.Source.ID)
	}
	return out
}

// TestFuseRankedPrefersAgreement checks reciprocal rank fusion: a hit both
// lists return outranks hits only one list found, ties keep first-seen order,
// the full-text copy of a shared hit is kept, and the limit applies.
func TestFuseRankedPrefersAgreement(t *testing.T) {
	t.Parallel()
	fts := []platformmcp.TeamMemoryHit{hit("chat", "a"), hit("memo", "b"), hit("chat", "c")}
	shared := hit("chat", "c")
	shared.Text = "vector copy"
	vector := []platformmcp.TeamMemoryHit{hit("digest", "d"), shared, hit("chat", "e")}

	fused := fuseRanked(fts, vector, 10)
	require.Equal(t, []string{"chat:c", "chat:a", "digest:d", "memo:b", "chat:e"}, ids(fused))
	require.Equal(t, "chat:c", fused[0].Text, "the full-text copy keeps its snippet")

	require.Equal(t, []string{"chat:c", "chat:a"}, ids(fuseRanked(fts, vector, 2)))
	require.Equal(t, []string{"chat:a", "memo:b", "chat:c"}, ids(fuseRanked(fts, nil, 10)), "one list keeps its order")
	require.Empty(t, fuseRanked(nil, nil, 10))
}

// TestVectorCandidatesFiltersAndRanks checks the vector list ranks by cosine
// similarity and applies the same kind and window filters as the tool.
func TestVectorCandidatesFiltersAndRanks(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	at := func(h platformmcp.TeamMemoryHit, when time.Time) platformmcp.TeamMemoryHit {
		h.At = when
		return h
	}
	items := []memoryItem{
		{hit: at(hit("chat", "near"), day.Add(time.Hour)), periodEnd: time.Time{}, vector: []float32{1, 0.1}},
		{hit: at(hit("chat", "far"), day.Add(2*time.Hour)), periodEnd: time.Time{}, vector: []float32{0, 1}},
		{hit: at(hit("memo", "mid"), day.Add(3*time.Hour)), periodEnd: time.Time{}, vector: []float32{1, 1}},
		{hit: at(hit("chat", "old"), day.AddDate(0, 0, -10)), periodEnd: time.Time{}, vector: []float32{1, 0}},
		// A week digest that started before the window but overlaps it.
		{hit: at(hit("digest", "week"), day.AddDate(0, 0, -3)), periodEnd: day.AddDate(0, 0, 4), vector: []float32{1, 0}},
	}
	query := []float32{1, 0}
	from, to := day, day.AddDate(0, 0, 1)

	require.Equal(t, []string{"digest:week", "chat:near", "memo:mid", "chat:far"}, ids(vectorCandidates(items, query, nil, from, to, 10)))
	require.Equal(t, []string{"digest:week", "chat:near"}, ids(vectorCandidates(items, query, nil, from, to, 2)))
	require.Equal(t, []string{"memo:mid"}, ids(vectorCandidates(items, query, []string{"memo"}, from, to, 10)))
}

func TestCosine(t *testing.T) {
	t.Parallel()
	require.InDelta(t, 1, cosine([]float32{2, 0}, []float32{5, 0}), 1e-9)
	require.InDelta(t, 0, cosine([]float32{1, 0}, []float32{0, 3}), 1e-9)
	require.Zero(t, cosine([]float32{0, 0}, []float32{1, 0}))
	require.Zero(t, cosine([]float32{1}, []float32{1, 0}), "mismatched dimensions score zero")
}
