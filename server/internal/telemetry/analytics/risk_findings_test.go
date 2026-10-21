package analytics

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
)

func TestRiskFindingsIsForOrgAdmins(t *testing.T) {
	t.Parallel()

	check, restricted := RiskFindings.AccessCheck("org-1")
	require.True(t, restricted)
	require.Equal(t, authz.ScopeOrgAdmin, check.Scope)
	require.Equal(t, "org-1", check.ResourceID)

	for _, ds := range []*Dataset{ToolCalls, Skills} {
		_, restricted := ds.AccessCheck("org-1")
		require.False(t, restricted, "%s is for anyone who can read the project", ds.Name)
	}
}

func TestRiskFindingsRefusesRows(t *testing.T) {
	t.Parallel()

	_, err := Compile(Default, Tenant{OrganizationID: "org", ProjectID: "proj"}, nil, Request{
		Dataset:      "risk_findings",
		FromUnixNano: 1,
		ToUnixNano:   2,
		Grain:        TimeGrainNone,
		Dimensions:   []string{"category"},
		Measures:     nil,
		Filters:      nil,
		OrderBy:      nil,
		Limit:        0,
		Ungrouped:    true,
	})
	var compileErr *Error
	require.ErrorAs(t, err, &compileErr)
	require.Equal(t, ErrAggregatesOnly, compileErr.Code)
}

// TestRiskFindingsReadsTheLiveCopy: the dataset reads each finding the way
// every risk surface does, so its counts agree with the Watchdog page.
func TestRiskFindingsReadsTheLiveCopy(t *testing.T) {
	t.Parallel()

	plan, err := Compile(Default, Tenant{OrganizationID: "org", ProjectID: "proj"}, nil, Request{
		Dataset:      "risk_findings",
		FromUnixNano: 1,
		ToUnixNano:   2,
		Grain:        TimeGrainDay,
		Dimensions:   []string{"category"},
		Measures:     []Measure{{Op: AggregationCount, Field: "", Alias: ""}},
		Filters:      nil,
		OrderBy:      nil,
		Limit:        0,
		Ungrouped:    false,
	})
	require.NoError(t, err)
	require.Equal(t, "risk_findings.risk_findings", plan.Name)
	require.Contains(t, plan.SQL, "ROW_NUMBER() OVER (PARTITION BY id ORDER BY "+chrepo.LatestCopyOrderSQL+") AS copy_rank FROM risk_findings")
	require.Contains(t, plan.SQL, chrepo.NotShadowCond)
	require.Contains(t, plan.SQL, "copy_rank = 1 AND "+chrepo.LiveStateCond+" AND "+chrepo.NotDeadLetterCond)
	require.Contains(t, plan.SQL, "created_at >= fromUnixTimestamp64Nano(toInt64(?), 'UTC')")
	require.Equal(t, []any{"org", "proj", int64(1), int64(2)}, plan.Args)
}

// TestRiskFindingsNamesNothingFound: no column the dataset reads can say what
// was matched or where it was, so no query over it can either.
func TestRiskFindingsNamesNothingFound(t *testing.T) {
	t.Parallel()

	for _, column := range riskFindingsSource.Columns {
		for _, sensitive := range []string{"match", "fingerprint", "description", "chat_id", "chat_message_id", "content_part_id", "tool_call_id", "path", "excluded_detail", "start_pos", "end_pos"} {
			require.NotContains(t, column, sensitive, "risk_findings reads %s", column)
		}
	}
}

func TestRiskFindingsSeverity(t *testing.T) {
	t.Parallel()

	severity, ok := RiskFindings.Field("severity")
	require.True(t, ok)

	t.Run("a finding with no scored policy reads as its category's band", func(t *testing.T) {
		t.Parallel()
		expr, args := readExpr(QueryContext{Tenant: Tenant{OrganizationID: "", ProjectID: ""}, Window: Window{FromUnixNano: 0, ToUnixNano: 0}, Lookups: nil}, severity)
		require.Nil(t, args)
		require.Equal(t, severity.LookupFallback, expr)
		require.Contains(t, expr, "transform(category, [")
		require.Contains(t, expr, "'secrets'")
		require.Contains(t, expr, ", 'medium')", "an unknown category reads as the default weight's band")
	})

	t.Run("a scored policy's band wins over the category's", func(t *testing.T) {
		t.Parallel()
		lookups := LookupMaps{RiskPolicySeveritiesLookup: {"policy-1": "critical"}}
		expr, args := readExpr(QueryContext{Tenant: Tenant{OrganizationID: "", ProjectID: ""}, Window: Window{FromUnixNano: 0, ToUnixNano: 0}, Lookups: lookups}, severity)
		require.Equal(t, "transform(risk_policy_id, ?, ?, "+severity.LookupFallback+")", expr)
		require.Equal(t, []any{[]string{"policy-1"}, []string{"critical"}}, args)
	})

	t.Run("a filter compares the band, not the policy", func(t *testing.T) {
		t.Parallel()
		plan, err := Compile(Default, Tenant{OrganizationID: "org", ProjectID: "proj"}, LookupMaps{RiskPolicySeveritiesLookup: {"policy-1": "high"}}, Request{
			Dataset:      "risk_findings",
			FromUnixNano: 1,
			ToUnixNano:   2,
			Grain:        TimeGrainNone,
			Dimensions:   nil,
			Measures:     []Measure{{Op: AggregationCount, Field: "", Alias: ""}},
			Filters:      []Filter{{Field: "severity", Operator: string(OperatorIn), Values: []string{"critical", "high"}}},
			OrderBy:      nil,
			Limit:        0,
			Ungrouped:    false,
		})
		require.NoError(t, err)
		require.Contains(t, plan.SQL, "transform(risk_policy_id, ?, ?, transform(category, [")
		require.Contains(t, plan.Args, "critical")
		require.Contains(t, plan.Args, "high")
	})
}

func TestRiskFindingsPolicyWithoutANameReadsAsEmpty(t *testing.T) {
	t.Parallel()

	policy, ok := RiskFindings.Field("policy")
	require.True(t, ok)
	expr, _ := readExpr(QueryContext{Tenant: Tenant{OrganizationID: "", ProjectID: ""}, Window: Window{FromUnixNano: 0, ToUnixNano: 0}, Lookups: nil}, policy)
	require.Equal(t, "''", expr, "a policy id is never shown in place of a name")
}

func TestSQLStringEscapes(t *testing.T) {
	t.Parallel()

	require.Equal(t, `'it\'s'`, sqlString("it's"))
	require.Equal(t, `'a\\b'`, sqlString(`a\b`))
	require.Equal(t, `['a', 'b']`, sqlStrings([]string{"a", "b"}))
}
