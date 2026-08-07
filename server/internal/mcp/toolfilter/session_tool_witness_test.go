package toolfilter

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func witnessStoreForTest(t *testing.T) *SessionToolWitnessStore {
	t.Helper()
	return NewSessionToolWitnessStore(testenv.NewLogger(t), testenv.NewMemoryCache())
}

func readOnlyTool(name string) WitnessedTool {
	return WitnessedTool{Name: name, Annotations: []string{AnnotationReadOnly}}
}

func TestSessionToolWitness_SinglePageAuthorizes(t *testing.T) {
	t.Parallel()

	s := witnessStoreForTest(t)
	s.WitnessPage(t.Context(), "grant-1", "sess-1", "", []WitnessedTool{readOnlyTool("get_a")}, "")

	require.True(t, s.MatchesWitnessed(t.Context(), "grant-1", "sess-1", "get_a", []string{AnnotationReadOnly}))
	require.False(t, s.MatchesWitnessed(t.Context(), "grant-1", "sess-1", "get_a", []string{AnnotationDestructive}))
	require.False(t, s.MatchesWitnessed(t.Context(), "grant-1", "sess-1", "get_b", []string{AnnotationReadOnly}))
	require.False(t, s.MatchesWitnessed(t.Context(), "grant-1", "other-sess", "get_a", []string{AnnotationReadOnly}), "witness is per session")
	require.False(t, s.MatchesWitnessed(t.Context(), "other-grant", "sess-1", "get_a", []string{AnnotationReadOnly}), "witness is per grant")
}

func TestSessionToolWitness_MidPaginationRowsAuthorize(t *testing.T) {
	t.Parallel()

	s := witnessStoreForTest(t)
	s.WitnessPage(t.Context(), "g", "s", "", []WitnessedTool{readOnlyTool("page1_tool")}, "cursor-2")

	// The client saw page 1; its tools are callable before pagination ends.
	require.True(t, s.MatchesWitnessed(t.Context(), "g", "s", "page1_tool", []string{AnnotationReadOnly}))

	s.WitnessPage(t.Context(), "g", "s", "cursor-2", []WitnessedTool{readOnlyTool("page2_tool")}, "")
	require.True(t, s.MatchesWitnessed(t.Context(), "g", "s", "page1_tool", []string{AnnotationReadOnly}))
	require.True(t, s.MatchesWitnessed(t.Context(), "g", "s", "page2_tool", []string{AnnotationReadOnly}))
}

func TestSessionToolWitness_FreshFirstPageReplacesWholesale(t *testing.T) {
	t.Parallel()

	s := witnessStoreForTest(t)
	s.WitnessPage(t.Context(), "g", "s", "", []WitnessedTool{readOnlyTool("old_tool")}, "")
	require.True(t, s.MatchesWitnessed(t.Context(), "g", "s", "old_tool", []string{AnnotationReadOnly}))

	s.WitnessPage(t.Context(), "g", "s", "", []WitnessedTool{readOnlyTool("new_tool")}, "")
	require.False(t, s.MatchesWitnessed(t.Context(), "g", "s", "old_tool", []string{AnnotationReadOnly}), "a re-list retires the prior witness")
	require.True(t, s.MatchesWitnessed(t.Context(), "g", "s", "new_tool", []string{AnnotationReadOnly}))
}

func TestSessionToolWitness_OutOfOrderCursorDropsWitness(t *testing.T) {
	t.Parallel()

	s := witnessStoreForTest(t)
	s.WitnessPage(t.Context(), "g", "s", "", []WitnessedTool{readOnlyTool("a")}, "cursor-2")
	s.WitnessPage(t.Context(), "g", "s", "stale-cursor", []WitnessedTool{readOnlyTool("b")}, "")

	require.False(t, s.MatchesWitnessed(t.Context(), "g", "s", "a", []string{AnnotationReadOnly}), "mismatch drops the whole witness")
	require.False(t, s.MatchesWitnessed(t.Context(), "g", "s", "b", []string{AnnotationReadOnly}))
}

func TestSessionToolWitness_ContinuationWithoutFirstPageIsIgnored(t *testing.T) {
	t.Parallel()

	s := witnessStoreForTest(t)
	s.WitnessPage(t.Context(), "g", "s", "cursor-2", []WitnessedTool{readOnlyTool("orphan")}, "")
	require.False(t, s.MatchesWitnessed(t.Context(), "g", "s", "orphan", []string{AnnotationReadOnly}))
}

func TestSessionToolWitness_DuplicateAndOversizeRowsDropWitness(t *testing.T) {
	t.Parallel()

	s := witnessStoreForTest(t)
	s.WitnessPage(t.Context(), "g", "s", "", []WitnessedTool{readOnlyTool("a"), readOnlyTool("a")}, "")
	require.False(t, s.MatchesWitnessed(t.Context(), "g", "s", "a", []string{AnnotationReadOnly}))

	s.WitnessPage(t.Context(), "g", "s2", "", []WitnessedTool{{Name: "", Annotations: nil}}, "")
	require.False(t, s.MatchesWitnessed(t.Context(), "g", "s2", "", []string{AnnotationReadOnly}))
}

func TestSessionToolWitness_MissingIdentityNeverMatches(t *testing.T) {
	t.Parallel()

	s := witnessStoreForTest(t)
	s.WitnessPage(t.Context(), "", "s", "", []WitnessedTool{readOnlyTool("a")}, "")
	s.WitnessPage(t.Context(), "g", "", "", []WitnessedTool{readOnlyTool("a")}, "")
	require.False(t, s.MatchesWitnessed(t.Context(), "", "s", "a", []string{AnnotationReadOnly}))
	require.False(t, s.MatchesWitnessed(t.Context(), "g", "", "a", []string{AnnotationReadOnly}))
}
