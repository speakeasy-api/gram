package analytics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/analytics"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// TestRiskFindingsIsForOrgAdminsOnly: a project member who is not an org
// admin reads the agent session datasets as before, never sees risk_findings
// in describe, and is refused when they name it anyway.
func TestRiskFindingsIsForOrgAdminsOnly(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{Scope: authz.ScopeProjectRead, Selector: authz.NewSelector(authz.ScopeProjectRead, ti.projectID)})

	described, err := ti.service.Describe(ctx, &gen.DescribePayload{SessionToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	names := make([]string, 0, len(described.Datasets))
	for _, ds := range described.Datasets {
		names = append(names, ds.Name)
	}
	require.Equal(t, []string{"tool_calls", "skills"}, names)
	require.Equal(t, []string{"risk_findings"}, described.RestrictedDatasets, "a widget over it can say why it shows nothing")

	from := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	to := time.Now().UTC().Format(time.RFC3339)
	_, err = ti.service.Query(ctx, &gen.QueryPayload{
		Dataset:  "risk_findings",
		From:     from,
		To:       to,
		Measures: []*gen.AnalyticsMeasure{{Op: "count", Field: nil, Alias: nil}},
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.DimensionValues(ctx, &gen.DimensionValuesPayload{Dataset: "risk_findings", Dimension: "category", From: from, To: to})
	requireOopsCode(t, err, oops.CodeForbidden)

	_, err = ti.service.Query(ctx, &gen.QueryPayload{
		Dataset:  "tool_calls",
		From:     from,
		To:       to,
		Measures: []*gen.AnalyticsMeasure{{Op: "count", Field: nil, Alias: nil}},
	})
	require.NoError(t, err, "a member still reads the agent session datasets")
}

func TestRiskFindingsAnswersAnOrgAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	from := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	to := time.Now().UTC().Format(time.RFC3339)
	result, err := ti.service.Query(ctx, &gen.QueryPayload{
		Dataset:    "risk_findings",
		From:       from,
		To:         to,
		Dimensions: []string{"severity", "policy"},
		Measures:   []*gen.AnalyticsMeasure{{Op: "count", Field: nil, Alias: nil}},
	})
	require.NoError(t, err, "the policy lookups load for a project with no policies")
	require.Equal(t, "risk_findings.risk_findings", result.Plan)
	require.Empty(t, result.Rows)

	_, err = ti.service.Query(ctx, &gen.QueryPayload{
		Dataset:    "risk_findings",
		From:       from,
		To:         to,
		Dimensions: []string{"category"},
		Ungrouped:  true,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}
