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
		TargetID:             "",
		Categories:           nil,
		UserEmails:           []string{"member@example.com"},
		ExactUserEmail:       "member@example.com",
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

// TestWithUserIdentityFilter_UnionsTheFoldWithTheResolvedSet pins that a
// populated canonical identity adds to the scope rather than replacing the
// caller-resolved one.
//
// The fold's id-keyed arm resolves an email through the identity_map, so an
// email the map does not carry resolves to no owner and matches nothing. The
// user ids the caller resolved from the rows themselves are the only thing that
// finds such a person, and discarding them in favour of the absent mapping is
// what made a per-person read report no activity for someone the grouped search
// had just returned as active.
func TestWithUserIdentityFilter_UnionsTheFoldWithTheResolvedSet(t *testing.T) {
	t.Parallel()

	// Deliberately distinguishable: two different ids, and an email that shares
	// no substring with either, so an assertion naming one cannot be satisfied
	// by another value in the fixture.
	const (
		firstID  = "user-42"
		secondID = "user-91"
		email    = "pat.rivera@example.com"
	)
	resolved := UserIdentity{UserIDs: []string{firstID, secondID}, Emails: []string{email}}
	canonical := CanonicalUserIdentity{OrgID: "org_0123456789", UserID: "", EmailLower: email}
	disabled := CanonicalUserIdentity{OrgID: "", UserID: "", EmailLower: ""}
	empty := UserIdentity{UserIDs: nil, Emails: nil}

	render := func(t *testing.T, identity UserIdentity, ident CanonicalUserIdentity) (string, []any) {
		t.Helper()
		query, args, err := withUserIdentityFilter(sq.Select("count()").From("telemetry_logs"), identity, ident).ToSql()
		require.NoError(t, err)
		return query, args
	}

	t.Run("both populated", func(t *testing.T) {
		t.Parallel()
		query, args := render(t, resolved, canonical)
		require.Contains(t, query, "joinGet('identity_map', 'canonical_user_id'", "the fold's owner lookup stays in the scope")
		require.Contains(t, args, firstID, "the resolved ids survive alongside the fold")
		require.Contains(t, args, secondID, "every resolved id survives, not just the first")
		require.Contains(t, args, email)
	})

	t.Run("canonical alone cannot name an unmapped email's rows", func(t *testing.T) {
		t.Parallel()
		// The regression itself, kept as the reason the union exists: with the
		// resolved set dropped, nothing in the scope names the person's ids.
		_, args := render(t, empty, canonical)
		require.NotContains(t, args, firstID)
		require.NotContains(t, args, secondID)
	})

	t.Run("literal alone stays literal", func(t *testing.T) {
		t.Parallel()
		query, args := render(t, resolved, disabled)
		require.NotContains(t, query, "joinGet", "a disabled canonical identity adds nothing")
		require.Contains(t, args, firstID)
	})

	t.Run("neither scopes to nobody", func(t *testing.T) {
		t.Parallel()
		query, _ := render(t, empty, disabled)
		require.NotContains(t, query, "WHERE", "an empty identity applies no user filter, as it always did")
	})
}
