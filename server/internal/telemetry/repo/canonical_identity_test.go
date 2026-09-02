package repo

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The skill-version path folds email filters through applySessionFilters like
// every other folded query, so it must carry the same condition-cache guard:
// per-granule predicate cache entries keyed on a joinGet over the mutable
// identity_map would survive a map swap and mis-filter rows until eviction.
func TestBuildSkillVersionMetricsQuery_CanonicalFoldDisablesConditionCache(t *testing.T) {
	t.Parallel()

	arg := AttributeMetricsQueryParams{
		ProjectIDs:           []string{"11111111-1111-1111-1111-111111111111"},
		TimeStart:            0,
		TimeEnd:              1,
		GroupBy:              "skill_version",
		SortBy:               "total_cost",
		Filters:              []AttributeMetricsFilter{{Dimension: "email", Values: []string{"person@example.com"}}},
		CanonicalIdentityOrg: "org_0123456789",
	}

	for _, timeseries := range []bool{false, true} {
		query, _, err := buildSkillVersionMetricsQuery(arg, timeseries)
		require.NoError(t, err)
		require.Contains(t, query, "joinGet('identity_map'", "email filter must fold on the skill-version path (timeseries=%v)", timeseries)
		require.True(t, strings.HasSuffix(query, "SETTINGS use_query_condition_cache = 0"), "folded skill-version query must disable the query condition cache (timeseries=%v), got: %s", timeseries, query)
	}

	literal := arg
	literal.CanonicalIdentityOrg = ""
	query, _, err := buildSkillVersionMetricsQuery(literal, false)
	require.NoError(t, err)
	require.NotContains(t, query, "joinGet", "flag-off query must stay literal")
	require.NotContains(t, query, "use_query_condition_cache", "flag-off query must not carry fold settings")
}

func TestBuildListAIDetectionSummariesQuery_CanonicalFoldDisablesConditionCache(t *testing.T) {
	t.Parallel()

	arg := ListAIDetectionSummariesParams{
		OrganizationID:       "org_0123456789",
		Categories:           nil,
		UserEmails:           []string{"member@example.com"},
		CanonicalIdentityOrg: "org_0123456789",
	}
	query, _, err := buildListAIDetectionSummariesQuery(arg)
	require.NoError(t, err)
	require.Contains(t, query, "joinGet('identity_map'")
	require.True(t, strings.HasSuffix(query, "SETTINGS use_query_condition_cache = 0"), "folded detection query must disable the query condition cache, got: %s", query)

	arg.CanonicalIdentityOrg = ""
	query, _, err = buildListAIDetectionSummariesQuery(arg)
	require.NoError(t, err)
	require.NotContains(t, query, "joinGet")
	require.NotContains(t, query, "use_query_condition_cache")
}
