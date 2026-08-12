package admin

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func seedProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, projectID, name, slug string) {
	t.Helper()

	err := testrepo.New(conn).CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{
		ID:             uuid.MustParse(projectID),
		Name:           name,
		Slug:           slug,
		OrganizationID: orgID,
	})
	require.NoError(t, err)
}

// seedTumRow plants an observed-traffic attribute_metrics_summaries row so the
// tracker's tokens-under-management read has volume to price. The TUM measure
// is input + output + cache-creation tokens.
func seedTumRow(t *testing.T, ctx context.Context, ch clickhouse.Conn, projectID string, bucket time.Time, hookSource string, input, output, cacheCreation int64) {
	t.Helper()

	err := ch.Exec(ctx, `
		INSERT INTO attribute_metrics_summaries (
			gram_project_id, time_bucket,
			department_name, job_title, employee_type, division_name, cost_center_name, user_email,
			model, hook_source, roles, groups,
			total_chats, total_input_tokens, total_output_tokens, total_tokens,
			cache_read_input_tokens, cache_creation_input_tokens, total_cost, total_tool_calls,
			account_type, provider, billing_mode,
			query_source, skill_name, agent_name, mcp_server_name, mcp_tool_name
		)
		SELECT
			toUUID(?), toDateTime(?, 'UTC'),
			'', '', '', '', '', '',
			'model', ?, [], [],
			uniqExactIfState('chat', toUInt8(0)),
			sumIfState(toInt64(?), toUInt8(1)),
			sumIfState(toInt64(?), toUInt8(1)),
			sumIfState(toInt64(0), toUInt8(0)),
			sumIfState(toInt64(0), toUInt8(0)),
			sumIfState(toInt64(?), toUInt8(1)),
			sumIfState(toFloat64(0), toUInt8(0)),
			countIfState(toUInt8(0)),
			'', '', '',
			'', '', '', '', ''
		FROM numbers(1)`,
		projectID, bucket.Unix(), hookSource, input, output, cacheCreation,
	)
	require.NoError(t, err)
}

// seedInferenceLog plants a raw telemetry_logs completion row carrying a
// gen_ai.usage.cost under the given hook source, the shape Gram-hosted
// inference spend is summed from.
func seedInferenceLog(t *testing.T, ctx context.Context, ch clickhouse.Conn, projectID string, ts time.Time, hookSource string, cost float64) {
	t.Helper()

	id, err := uuid.NewV7()
	require.NoError(t, err)

	attrs := `{"gram.hook.source":"` + hookSource + `","gen_ai.usage.cost":` + strconv.FormatFloat(cost, 'f', -1, 64) + `}`

	err = ch.Exec(ctx, `
		INSERT INTO telemetry_logs (
			id, time_unix_nano, observed_time_unix_nano, severity_text, body,
			trace_id, span_id, attributes, resource_attributes,
			gram_project_id, gram_urn, service_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id.String(), ts.UnixNano(), ts.UnixNano(), "INFO", "chat completion",
		nil, nil, attrs, "{}",
		projectID, "completions:usage", "gram-server")
	require.NoError(t, err)
}

func rowByOrg(t *testing.T, rows []*gen.AdminPricingTrackerRow, orgID string) *gen.AdminPricingTrackerRow {
	t.Helper()
	for _, r := range rows {
		if r.OrganizationID == orgID {
			return r
		}
	}
	t.Fatalf("no pricing tracker row for org %q", orgID)
	return nil
}

func TestListPricingTracker_RollsUpSpendAndPrice(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, ch := newTestAdminServiceWithClickhouse(t)

	bucket := time.Now().UTC().Add(-24 * time.Hour)

	// Enterprise org with two projects: TUM and inference spend must roll up
	// across both projects onto the org.
	orgA := "org_pt_ent_" + uuid.NewString()[:8]
	seedOrg(t, ctx, conn, orgFixture{id: orgA, name: "Enterprise Co", slug: "ent-" + uuid.NewString()[:8], accountType: "enterprise", whitelisted: true})
	projA1 := uuid.NewString()
	projA2 := uuid.NewString()
	seedProject(t, ctx, conn, orgA, projA1, "A1", "a1-"+uuid.NewString()[:8])
	seedProject(t, ctx, conn, orgA, projA2, "A2", "a2-"+uuid.NewString()[:8])
	// 600k + 400k = 1,000,000 monthly tokens → PAYG first band $0.35/M = $0.35.
	seedTumRow(t, ctx, ch, projA1, bucket, "claude-code", 600_000, 0, 0)
	seedTumRow(t, ctx, ch, projA2, bucket, "cursor", 400_000, 0, 0)
	// Inference spend: 1.00 (playground) + 0.75 (risk-analysis) = 1.75.
	seedInferenceLog(t, ctx, ch, projA1, bucket, "playground", 1.00)
	seedInferenceLog(t, ctx, ch, projA2, bucket, "risk-analysis", 0.75)
	// Observed agent traffic cost must NOT count as inference spend.
	seedInferenceLog(t, ctx, ch, projA1, bucket, "claude-code", 99.0)

	// Pro org, single project, smaller footprint.
	orgB := "org_pt_pro_" + uuid.NewString()[:8]
	seedOrg(t, ctx, conn, orgFixture{id: orgB, name: "Pro Co", slug: "pro-" + uuid.NewString()[:8], accountType: "pro", whitelisted: true})
	projB := uuid.NewString()
	seedProject(t, ctx, conn, orgB, projB, "B", "b-"+uuid.NewString()[:8])
	seedTumRow(t, ctx, ch, projB, bucket, "claude-code", 500_000, 0, 0)
	seedInferenceLog(t, ctx, ch, projB, bucket, "elements", 0.50)

	// Free org: excluded by default.
	orgFree := "org_pt_free_" + uuid.NewString()[:8]
	seedOrg(t, ctx, conn, orgFixture{id: orgFree, name: "Free Co", slug: "free-" + uuid.NewString()[:8], accountType: "free", whitelisted: true})
	projFree := uuid.NewString()
	seedProject(t, ctx, conn, orgFree, projFree, "F", "f-"+uuid.NewString()[:8])
	seedInferenceLog(t, ctx, ch, projFree, bucket, "playground", 5.0)

	res, err := svc.ListPricingTracker(ctx, &gen.ListPricingTrackerPayload{})
	require.NoError(t, err)
	require.True(t, res.InferenceSpendAvailable)
	require.NotEmpty(t, res.WindowStart)
	require.NotEmpty(t, res.WindowEnd)

	// Free org excluded; only the two paying orgs seeded here appear.
	rowA := rowByOrg(t, res.Rows, orgA)
	rowB := rowByOrg(t, res.Rows, orgB)
	for _, r := range res.Rows {
		require.NotEqual(t, orgFree, r.OrganizationID, "free org should be excluded by default")
	}

	require.Equal(t, "enterprise", rowA.AccountType)
	require.Equal(t, int64(1_000_000), rowA.MonthlyTumTokens)
	require.InDelta(t, 0.35, rowA.PaygMonthlyPrice, 1e-6)
	require.InDelta(t, 0.35, rowA.PaygEffectiveRatePerMillion, 1e-6)
	require.InDelta(t, 1.75, rowA.InferenceSpend, 1e-6, "observed claude-code cost must be excluded")

	require.Equal(t, "pro", rowB.AccountType)
	require.Equal(t, int64(500_000), rowB.MonthlyTumTokens)
	require.InDelta(t, 0.50, rowB.InferenceSpend, 1e-6)

	// Highest inference spend sorts first.
	require.Equal(t, orgA, res.Rows[0].OrganizationID)
}

func TestListPricingTracker_IncludeFreeAndAccountTypeFilter(t *testing.T) {
	t.Parallel()

	ctx, svc, conn, ch := newTestAdminServiceWithClickhouse(t)

	bucket := time.Now().UTC().Add(-24 * time.Hour)

	orgEnt := "org_ptf_ent_" + uuid.NewString()[:8]
	seedOrg(t, ctx, conn, orgFixture{id: orgEnt, name: "Ent", slug: "e-" + uuid.NewString()[:8], accountType: "enterprise", whitelisted: true})
	projEnt := uuid.NewString()
	seedProject(t, ctx, conn, orgEnt, projEnt, "E", "e-"+uuid.NewString()[:8])
	seedInferenceLog(t, ctx, ch, projEnt, bucket, "playground", 1.0)

	orgFree := "org_ptf_free_" + uuid.NewString()[:8]
	seedOrg(t, ctx, conn, orgFixture{id: orgFree, name: "Free", slug: "f-" + uuid.NewString()[:8], accountType: "free", whitelisted: true})
	projFree := uuid.NewString()
	seedProject(t, ctx, conn, orgFree, projFree, "F", "f-"+uuid.NewString()[:8])
	seedInferenceLog(t, ctx, ch, projFree, bucket, "playground", 2.0)

	// include_free surfaces the free org.
	includeFree := true
	res, err := svc.ListPricingTracker(ctx, &gen.ListPricingTrackerPayload{IncludeFree: &includeFree})
	require.NoError(t, err)
	_ = rowByOrg(t, res.Rows, orgFree)
	_ = rowByOrg(t, res.Rows, orgEnt)

	// account_type filter narrows to a single tier.
	entType := "enterprise"
	res, err = svc.ListPricingTracker(ctx, &gen.ListPricingTrackerPayload{AccountType: &entType})
	require.NoError(t, err)
	_ = rowByOrg(t, res.Rows, orgEnt)
	for _, r := range res.Rows {
		require.Equal(t, "enterprise", r.AccountType)
	}
}

func TestListPricingTracker_WithoutClickhouse(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)

	orgEnt := "org_ptn_ent_" + uuid.NewString()[:8]
	seedOrg(t, ctx, conn, orgFixture{id: orgEnt, name: "Ent", slug: "en-" + uuid.NewString()[:8], accountType: "enterprise", whitelisted: true})
	projEnt := uuid.NewString()
	seedProject(t, ctx, conn, orgEnt, projEnt, "E", "en-"+uuid.NewString()[:8])

	res, err := svc.ListPricingTracker(ctx, &gen.ListPricingTrackerPayload{})
	require.NoError(t, err)
	require.False(t, res.InferenceSpendAvailable)

	row := rowByOrg(t, res.Rows, orgEnt)
	require.Equal(t, int64(0), row.MonthlyTumTokens)
	require.InDelta(t, 0.0, row.PaygMonthlyPrice, 1e-9)
	require.InDelta(t, 0.0, row.InferenceSpend, 1e-9)
}
