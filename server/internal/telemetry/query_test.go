package telemetry_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/stretchr/testify/require"
)

// insertAttributeUsageLog inserts a usage-metrics telemetry row carrying the
// user/request attributes the attribute_metrics_summaries MV breaks down by.
func insertAttributeUsageLog(t *testing.T, ctx context.Context, projectID string, timestamp time.Time, chatID string, cost float64, totalTokens int, model, provider, email, department string, roles []string) {
	t.Helper()

	conn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	id, err := uuid.NewV7()
	require.NoError(t, err)

	usageURN := provider + ":usage:metrics"
	attributes := map[string]any{
		"gen_ai.conversation.id":          chatID,
		"gen_ai.usage.input_tokens":       totalTokens,
		"gen_ai.usage.total_tokens":       totalTokens,
		"gen_ai.usage.cost":               cost,
		"gen_ai.response.model":           model,
		"gram.hook.source":                provider,
		"gram.resource.urn":               usageURN,
		"user.email":                      email,
		"user.attributes.department_name": department,
	}
	if roles != nil {
		attributes["user.roles"] = roles
	}

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = conn.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), timestamp.UnixNano(), timestamp.UnixNano(), "INFO", "chat completion",
		nil, nil, string(attrsJSON), "{}",
		projectID, usageURN, "gram-agents")
	require.NoError(t, err)
}

func tableCostByGroup(rows []*gen.QueryRow) map[string]float64 {
	out := make(map[string]float64, len(rows))
	for _, r := range rows {
		out[r.GroupValue] = r.Measures.TotalCost
	}
	return out
}

func rowByGroup(t *testing.T, rows []*gen.QueryRow, group string) *gen.QueryRow {
	t.Helper()
	for _, r := range rows {
		if r.GroupValue == group {
			return r
		}
	}
	t.Fatalf("row for group %q not found", group)
	return nil
}

func TestQuery_GroupByDimensionsAndDrilldown(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	// Org-scoped read grant for telemetry.query.
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	now := time.Date(2026, time.June, 20, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)

	// Engineering: admin+dev ($0.25) and dev ($0.10). Sales: no roles ($0.50).
	insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), 0.25, 15, "opus", "claude-code", "a@x.com", "Engineering", []string{"admin", "dev"})
	insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), 0.10, 5, "opus", "cursor", "b@x.com", "Engineering", []string{"dev"})
	insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), 0.50, 50, "sonnet", "claude-code", "c@x.com", "Sales", nil)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Group by department (eventual consistency on the MV).
	var deptResult *gen.QueryResult
	require.Eventually(t, func() bool {
		res, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:    from,
			To:      to,
			GroupBy: conv.PtrEmpty("department_name"),
			TopN:    10,
			SortBy:  "total_cost",
		})
		if err != nil || res == nil {
			return false
		}
		deptResult = res
		return len(res.Table) == 2
	}, 10*time.Second, 200*time.Millisecond)

	require.Equal(t, "department_name", deptResult.GroupBy)
	require.Equal(t, int64(3600), deptResult.IntervalSeconds)

	// Ordered by cost desc: Sales ($0.50) before Engineering ($0.35).
	require.Equal(t, "Sales", deptResult.Table[0].GroupValue)
	require.Equal(t, "Engineering", deptResult.Table[1].GroupValue)
	deptCost := tableCostByGroup(deptResult.Table)
	require.InDelta(t, 0.50, deptCost["Sales"], 1e-9)
	require.InDelta(t, 0.35, deptCost["Engineering"], 1e-9)

	// One timeseries series per table row, gap-filled.
	require.Len(t, deptResult.Timeseries, 2)
	for _, s := range deptResult.Timeseries {
		require.NotEmpty(t, s.Points)
	}

	// dimension_values: each group carries the distinct values of every other
	// allowlisted dimension observed within it. Engineering had two users
	// (a@x.com on opus/claude-code with roles admin+dev, b@x.com on opus/cursor
	// with role dev). The group_by dimension (department_name) is absent.
	eng := rowByGroup(t, deptResult.Table, "Engineering")
	require.NotContains(t, eng.DimensionValues, "department_name", "group_by dimension must be excluded")
	require.ElementsMatch(t, []string{"a@x.com", "b@x.com"}, eng.DimensionValues["email"])
	require.ElementsMatch(t, []string{"opus"}, eng.DimensionValues["model"])
	require.ElementsMatch(t, []string{"claude-code", "cursor"}, eng.DimensionValues["hook_source"])
	require.ElementsMatch(t, []string{"admin", "dev"}, eng.DimensionValues["role"])
	// Unset dimensions are present as keys with empty (filtered) lists.
	require.Empty(t, eng.DimensionValues["job_title"])

	// Sales had a single role-less user; its email surfaces and role is empty.
	sales := rowByGroup(t, deptResult.Table, "Sales")
	require.ElementsMatch(t, []string{"c@x.com"}, sales.DimensionValues["email"])
	require.Empty(t, sales.DimensionValues["role"])

	// Group by role: dev gets both Engineering rows ($0.35), admin one ($0.25),
	// and Sales' role-less spend surfaces under the empty-string group ($0.50).
	roleResult, err := ti.service.Query(ctx, &gen.QueryPayload{
		From:    from,
		To:      to,
		GroupBy: conv.PtrEmpty("role"),
		TopN:    10,
		SortBy:  "total_cost",
	})
	require.NoError(t, err)
	roleCost := tableCostByGroup(roleResult.Table)
	require.InDelta(t, 0.35, roleCost["dev"], 1e-9)
	require.InDelta(t, 0.25, roleCost["admin"], 1e-9)
	require.InDelta(t, 0.50, roleCost[""], 1e-9)

	// Drill-down: filter department=Engineering, group by role. Only Engineering
	// rows count, so dev $0.35 and admin $0.25 (no role-less Sales spend).
	drillResult, err := ti.service.Query(ctx, &gen.QueryPayload{
		From:    from,
		To:      to,
		GroupBy: conv.PtrEmpty("role"),
		Filters: []*gen.QueryFilter{{Dimension: "department_name", Values: []string{"Engineering"}}},
		TopN:    10,
		SortBy:  "total_cost",
	})
	require.NoError(t, err)
	drillCost := tableCostByGroup(drillResult.Table)
	require.InDelta(t, 0.35, drillCost["dev"], 1e-9)
	require.InDelta(t, 0.25, drillCost["admin"], 1e-9)
	require.NotContains(t, drillCost, "", "role-less Sales spend must be excluded by the department filter")

	// No group_by: a single aggregate row over the whole org slice ($0.85).
	totalResult, err := ti.service.Query(ctx, &gen.QueryPayload{
		From:   from,
		To:     to,
		TopN:   10,
		SortBy: "total_cost",
	})
	require.NoError(t, err)
	require.Len(t, totalResult.Table, 1)
	require.Empty(t, totalResult.Table[0].GroupValue)
	require.InDelta(t, 0.85, totalResult.Table[0].Measures.TotalCost, 1e-9)
	require.Len(t, totalResult.Timeseries, 1)
}

func TestQuery_DefaultSortByAndTopN(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	now := time.Date(2026, time.June, 20, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)
	for i := range 12 {
		dept := "D" + strconv.Itoa(i+1)
		cost := float64(12 - i)
		insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), cost, 1, "m", "claude-code", dept+"@x.com", dept, nil)
	}

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	var res *gen.QueryResult
	require.Eventually(t, func() bool {
		r, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:    from,
			To:      to,
			GroupBy: conv.PtrEmpty("department_name"),
		})
		if err != nil || r == nil {
			return false
		}
		res = r
		return len(r.Table) == 11
	}, 10*time.Second, 200*time.Millisecond)

	require.Equal(t, "D1", res.Table[0].GroupValue, "default sort_by should rank by total_cost")
	require.Equal(t, "D10", res.Table[9].GroupValue)
	require.Equal(t, "Other", res.Table[10].GroupValue, "default top_n should keep 10 groups and roll up the rest")
	require.InDelta(t, 3.0, res.Table[10].Measures.TotalCost, 1e-9)
}

func TestQuery_TopNRollupIntoOther(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := authCtx.ProjectID.String()

	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})

	now := time.Date(2026, time.June, 20, 1, 0, 0, 0, time.UTC)
	ts := now.Add(-10 * time.Minute)

	// Four departments with distinct costs; top_n=2 keeps the two priciest and
	// folds the rest into "Other".
	insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), 4.0, 1, "m", "claude-code", "a@x.com", "D1", nil)
	insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), 3.0, 1, "m", "claude-code", "b@x.com", "D2", nil)
	insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), 2.0, 1, "m", "claude-code", "c@x.com", "D3", nil)
	insertAttributeUsageLog(t, ctx, projectID, ts, uuid.NewString(), 1.0, 1, "m", "claude-code", "d@x.com", "D4", nil)

	from := now.Add(-1 * time.Hour).Format(time.RFC3339)
	to := now.Add(1 * time.Hour).Format(time.RFC3339)

	var res *gen.QueryResult
	require.Eventually(t, func() bool {
		r, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:    from,
			To:      to,
			GroupBy: conv.PtrEmpty("department_name"),
			TopN:    2,
			SortBy:  "total_cost",
		})
		if err != nil || r == nil {
			return false
		}
		res = r
		// 4 distinct departments visible before rollup.
		var total float64
		for _, row := range r.Table {
			total += row.Measures.TotalCost
		}
		return total >= 9.99 && len(r.Table) == 3
	}, 10*time.Second, 200*time.Millisecond)

	require.Equal(t, "D1", res.Table[0].GroupValue)
	require.Equal(t, "D2", res.Table[1].GroupValue)
	require.Equal(t, "Other", res.Table[2].GroupValue)
	require.InDelta(t, 3.0, res.Table[2].Measures.TotalCost, 1e-9) // D3 + D4

	// Timeseries has a matching Other series.
	require.Len(t, res.Timeseries, 3)
	var hasOther bool
	for _, s := range res.Timeseries {
		if s.GroupValue == "Other" {
			hasOther = true
		}
	}
	require.True(t, hasOther)
}
