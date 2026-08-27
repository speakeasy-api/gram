package repo

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChallengeBucketResolutionQuerySizeIndependentOfResolutionCount(t *testing.T) {
	t.Parallel()

	resolved := true
	queryFor := func(ids []string) (string, []any) {
		t.Helper()

		filters := ChallengeBucketFilters{
			OrganizationID:       "org-id",
			ProjectID:            nil,
			Outcome:              nil,
			PrincipalURN:         nil,
			Scope:                nil,
			MemberUserIDs:        nil,
			Resolved:             &resolved,
			ResolvedChallengeIDs: ids,
			Limit:                20,
			Offset:               0,
		}
		sb := sq.Select("1").
			From("authz_challenge_bucket_summaries").
			GroupBy(challengeBucketGroupBy)
		sb = challengeBucketWhere(sb, filters)
		sb = challengeBucketResolution(sb, filters)

		query, args, err := sb.ToSql()
		require.NoError(t, err)
		return query, args
	}

	largeSet := make([]string, 8_000)
	for i := range largeSet {
		largeSet[i] = fmt.Sprintf("challenge-%d", i)
	}

	smallQuery, smallArgs := queryFor([]string{"challenge-1"})
	largeQuery, largeArgs := queryFor(largeSet)
	require.Equal(t, smallQuery, largeQuery)
	require.Equal(t, smallArgs, largeArgs)
	require.NotContains(t, largeQuery, largeSet[len(largeSet)-1])
}
