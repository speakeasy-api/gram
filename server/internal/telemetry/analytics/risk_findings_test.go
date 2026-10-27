package analytics

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskRepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
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

// TestRiskFindingsReadsTheFoldedTable: the dataset reads one folded row per
// finding and keeps the live ones, so nothing is sorted at read time.
func TestRiskFindingsReadsTheFoldedTable(t *testing.T) {
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
	require.Contains(t, plan.SQL, "FROM risk_finding_states WHERE organization_id = ? AND project_id = ? AND id IN (SELECT id FROM risk_finding_states")
	require.Contains(t, plan.SQL, "HAVING min(created_at_unix_nano) >= ? AND min(created_at_unix_nano) < ? AND argMaxMerge(live) = 1")
	require.NotContains(t, plan.SQL, "ROW_NUMBER")
	require.NotContains(t, plan.SQL, "LIMIT 1 BY")
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
	require.Equal(t, RiskPolicySeveritiesLookup, severity.Lookup)

	t.Run("it looks a finding up by its policy and its category's band", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, riskSeverityKeyExpr, severity.Expr)
		require.True(t, strings.HasPrefix(severity.Expr, "concat(risk_policy_id, ':', transform(category, ["))
		require.Contains(t, severity.Expr, "'secrets'")
		require.True(t, strings.HasSuffix(severity.Expr, ", 'medium'))"), "an unknown category reads as the default weight's band")
	})

	live := uuid.New()
	unscored := uuid.New()
	deleted := uuid.New()
	severities := severitiesByKey([]riskRepo.ListRiskPolicyAnalyticsLabelsRow{
		{ID: live, Name: "Secrets", Score: 9.5, Deleted: false},
		{ID: unscored, Name: "Unscored", Score: 0, Deleted: false},
		{ID: deleted, Name: "Old", Score: 9.5, Deleted: true},
	})

	t.Run("a live, scored policy's band wins over the category's", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "critical", severities[live.String()+":medium"])
		require.Equal(t, "critical", severities[live.String()+":high"])
	})

	t.Run("a finding with no scored policy reads as its category's band", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "high", severities[":high"], "no policy")
		require.Equal(t, "medium", severities[unscored.String()+":medium"], "a policy with no score")
		require.Equal(t, "high", severities[deleted.String()+":high"], "a deleted policy contributes no score")
	})

	t.Run("every key a finding can have reads as a band", func(t *testing.T) {
		t.Parallel()
		for _, policy := range []string{"", live.String(), unscored.String(), deleted.String()} {
			for _, band := range riskCategoryBands() {
				require.Contains(t, []string{"critical", "high", "medium", "low"}, severities[policy+":"+band], "%s:%s", policy, band)
			}
		}
		fallbacks, unknown := risk.CategoryFallbackSeverities()
		for category, band := range fallbacks {
			require.Contains(t, riskCategoryBands(), band, category)
		}
		require.Contains(t, riskCategoryBands(), unknown)
	})

	t.Run("a filter compares the band, not the key", func(t *testing.T) {
		t.Parallel()
		plan, err := Compile(Default, Tenant{OrganizationID: "org", ProjectID: "proj"}, LookupMaps{RiskPolicySeveritiesLookup: severities}, Request{
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
		require.Contains(t, plan.SQL, "transform("+riskSeverityKeyExpr+", ?, ?, "+riskSeverityKeyExpr+") IN (?,?)")
		require.Contains(t, plan.Args, "critical")
		require.Contains(t, plan.Args, "high")
	})
}

func TestRiskPolicyNamesMarkTheDeleted(t *testing.T) {
	t.Parallel()

	live, deleted := uuid.New(), uuid.New()
	names := namesByPolicy([]riskRepo.ListRiskPolicyAnalyticsLabelsRow{
		{ID: live, Name: "Secrets", Score: 9.5, Deleted: false},
		{ID: deleted, Name: "Old", Score: 0, Deleted: true},
	})
	require.Equal(t, map[string]string{live.String(): "Secrets", deleted.String(): "Old (deleted)"}, names)
}

func TestSQLStringEscapes(t *testing.T) {
	t.Parallel()

	require.Equal(t, `'it\'s'`, sqlString("it's"))
	require.Equal(t, `'a\\b'`, sqlString(`a\b`))
	require.Equal(t, `['a', 'b']`, sqlStrings([]string{"a", "b"}))
}
