package annotation_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/sqlclint/annotation"
)

func TestFindReadsCategoryAndReason(t *testing.T) {
	t.Parallel()

	got, ok := annotation.Find([]string{"sqlclint:ignore admin -- staff console only"}, 5)
	require.True(t, ok)
	require.Equal(t, "admin", got.Category)
	require.Equal(t, "staff console only", got.Reason)
	require.Equal(t, 5, got.Line)
}

func TestFindSkipsUnrelatedComments(t *testing.T) {
	t.Parallel()

	got, ok := annotation.Find([]string{
		"Fetches one row.",
		"sqlclint:ignore token-keyed -- key_hash is the credential",
	}, 10)
	require.True(t, ok)
	require.Equal(t, "token-keyed", got.Category)
	require.Equal(t, 11, got.Line, "line must point at the annotation, not the comment block")
}

func TestFindJoinsAWrappedReason(t *testing.T) {
	t.Parallel()

	got, ok := annotation.Find([]string{
		"sqlclint:ignore background-sweep -- outbox GC activity; runs on a timer",
		"with no request context and deletes only processed rows",
	}, 1)
	require.True(t, ok)
	require.Equal(t, "outbox GC activity; runs on a timer with no request context and deletes only processed rows", got.Reason)
}

// The reason is what a reviewer checks, so its absence must be visible to the
// caller rather than silently defaulted.
func TestFindReportsAnEmptyReason(t *testing.T) {
	t.Parallel()

	got, ok := annotation.Find([]string{"sqlclint:ignore admin"}, 1)
	require.True(t, ok)
	require.Equal(t, "admin", got.Category)
	require.Empty(t, got.Reason)

	got, ok = annotation.Find([]string{"sqlclint:ignore admin --   "}, 1)
	require.True(t, ok)
	require.Empty(t, got.Reason)
}

func TestFindReportsAnEmptyCategory(t *testing.T) {
	t.Parallel()

	got, ok := annotation.Find([]string{"sqlclint:ignore -- no category given"}, 1)
	require.True(t, ok)
	require.Empty(t, got.Category)
}

// A query claiming two exemptions is contradicting itself; the second must not
// be folded into the first one's reason.
func TestFindStopsAtASecondAnnotation(t *testing.T) {
	t.Parallel()

	got, ok := annotation.Find([]string{
		"sqlclint:ignore admin -- staff console",
		"sqlclint:ignore global-table -- also this",
	}, 1)
	require.True(t, ok)
	require.Equal(t, "admin", got.Category)
	require.Equal(t, "staff console", got.Reason)
}

func TestFindReturnsFalseWhenAbsent(t *testing.T) {
	t.Parallel()

	_, ok := annotation.Find([]string{"Fetches one row.", "Nothing to see."}, 1)
	require.False(t, ok)

	_, ok = annotation.Find(nil, 1)
	require.False(t, ok)
}
