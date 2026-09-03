package repo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The identity pages hand the challenge listing the same window every other
// panel reads, so the bounds have to reach the query — and reach it as nanos,
// since the column is DateTime64(9) and a bound time.Time would be truncated
// to the second on the way out.
func TestChallengeWhereWindow(t *testing.T) {
	t.Parallel()

	base := ChallengeFilters{
		OrganizationID: "org-id",
		ProjectID:      nil,
		Outcome:        nil,
		PrincipalURN:   nil,
		Scope:          nil,
		MemberUserIDs:  nil,
	}
	from := time.Date(2026, 3, 1, 12, 0, 0, 123456789, time.UTC)
	to := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	queryFor := func(f ChallengeListFilters) (string, []any) {
		t.Helper()
		query, args, err := challengeWhere(sq.Select("1").From("authz_challenges"), f).ToSql()
		require.NoError(t, err)
		return query, args
	}

	filters := ChallengeListFilters{
		ChallengeFilters: base,
		From:             nil,
		To:               nil,
		Limit:            50,
		Offset:           0,
		SkipPagination:   false,
	}

	query, args := queryFor(filters)
	require.NotContains(t, query, "timestamp", "no bound is no filter")
	require.Equal(t, []any{"org-id"}, args)

	filters.From = &from
	query, args = queryFor(filters)
	require.Contains(t, query, "timestamp >= fromUnixTimestamp64Nano(?)")
	require.NotContains(t, query, "timestamp <")
	require.Equal(t, []any{"org-id", from.UnixNano()}, args)

	filters.To = &to
	query, args = queryFor(filters)
	require.Contains(t, query, "timestamp >= fromUnixTimestamp64Nano(?)")
	require.Contains(t, query, "timestamp < fromUnixTimestamp64Nano(?)", "the upper bound is exclusive")
	require.Equal(t, []any{"org-id", from.UnixNano(), to.UnixNano()}, args)

	// Nanosecond precision survives the round trip; truncating here would move
	// the boundary off the instant the caller asked for.
	require.Equal(t, int64(123456789), from.UnixNano()%int64(time.Second))
}
