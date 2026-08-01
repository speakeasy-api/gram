//go:build live_email

package activities_test

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/email"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	"github.com/speakeasy-api/gram/server/internal/usage"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

// insertWeeklyUsageComponents seeds one observed session's aggregate row into
// attribute_metrics_summaries with distinct per-component token counts so the
// weekly summary's total exercises every column of the TUM measure. Like
// insertStoredSession, it writes the aggregate row directly (bypassing the
// MV's ingestion cutoff) with generation 0 / is_active 1 to match a live row.
func insertWeeklyUsageComponents(t *testing.T, ctx context.Context, chConn clickhouse.Conn, projectID string, timestamp time.Time, input, output, cacheWrite, cacheRead int64) {
	t.Helper()

	err := chConn.Exec(ctx, `
		INSERT INTO attribute_metrics_summaries (
			gram_project_id, time_bucket,
			department_name, job_title, employee_type, division_name, cost_center_name,
			user_email, model, hook_source, roles, groups,
			total_chats, total_input_tokens, total_output_tokens, total_tokens,
			cache_read_input_tokens, cache_creation_input_tokens, total_cost,
			total_tool_calls, unique_tool_calls,
			account_type, provider, billing_mode,
			query_source, skill_name, agent_name, mcp_server_name, mcp_tool_name,
			generation, is_active, hook_hostname,
			total_work_units, scored_cost, scored_tokens
		)
		SELECT
			toUUID(?) AS gram_project_id,
			toStartOfHour(fromUnixTimestamp64Nano(?)) AS time_bucket,
			'' AS department_name, '' AS job_title, '' AS employee_type,
			'' AS division_name, '' AS cost_center_name,
			'' AS user_email, 'claude-4.6' AS model, 'claude-code' AS hook_source,
			[]::Array(String) AS roles, []::Array(String) AS groups,
			uniqExactIfState(toString('weekly-summary-session'), toUInt8(1)) AS total_chats,
			sumIfState(toInt64(?), toUInt8(1)) AS total_input_tokens,
			sumIfState(toInt64(?), toUInt8(1)) AS total_output_tokens,
			sumIfState(toInt64(?), toUInt8(1)) AS total_tokens,
			sumIfState(toInt64(?), toUInt8(1)) AS cache_read_input_tokens,
			sumIfState(toInt64(?), toUInt8(1)) AS cache_creation_input_tokens,
			sumIfState(toFloat64(0), toUInt8(1)) AS total_cost,
			countIfState(toUInt8(0)) AS total_tool_calls,
			uniqExactIfState(toString(''), toUInt8(0)) AS unique_tool_calls,
			'' AS account_type, '' AS provider, '' AS billing_mode,
			'' AS query_source, '' AS skill_name, '' AS agent_name,
			'' AS mcp_server_name, '' AS mcp_tool_name,
			toUInt8(0) AS generation, toUInt8(1) AS is_active,
			'' AS hook_hostname,
			sumIfState(toFloat64(0), toUInt8(0)) AS total_work_units,
			sumIfState(toFloat64(0), toUInt8(0)) AS scored_cost,
			sumIfState(toInt64(0), toUInt8(0)) AS scored_tokens
	`, projectID, timestamp.UnixNano(), input, output, input+output+cacheWrite+cacheRead, cacheRead, cacheWrite)
	require.NoError(t, err)
}

// TestWeeklyUsageSummary_LiveSendThroughLoops drives the production send path
// end to end — target resolution from Postgres, per-component TUM totals from
// ClickHouse, row rendering, and a real Loops transactional API call — so the
// delivered email can be inspected in an inbox before shipping.
//
// It is a manually run smoke test behind the live_email build tag, so CI and
// regular local runs never compile or send it. Run it with:
//
//	LOOPS_API_KEY=... WEEKLY_USAGE_SUMMARY_SMOKE_RECIPIENT=you@example.com \
//	  mise exec -- go test -tags live_email ./internal/background/activities/ -run TestWeeklyUsageSummary_LiveSendThroughLoops -v
//
// Because it exercises the real template, it also proves Loops accepts the
// exact variable set the server sends.
func TestWeeklyUsageSummary_LiveSendThroughLoops(t *testing.T) {
	t.Parallel()

	apiKey := os.Getenv("LOOPS_API_KEY")
	require.NotEmpty(t, apiKey, "LOOPS_API_KEY must be set to run the live email smoke test")
	require.NotEqual(t, "unset", apiKey, "LOOPS_API_KEY must be a real key to run the live email smoke test")
	recipient := os.Getenv("WEEKLY_USAGE_SUMMARY_SMOKE_RECIPIENT")
	require.NotEmpty(t, recipient, "WEEKLY_USAGE_SUMMARY_SMOKE_RECIPIENT must be set to the inbox that receives the smoke test email")

	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, "weekly_usage_summary_live")
	require.NoError(t, err)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	orgID := "org-" + uuid.NewString()[:8]
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Weekly Summary Smoke Test Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Weekly Summary Smoke Test Project",
		Slug:           "proj-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	const anchorDay = 1
	_, err = usagerepo.New(conn).UpsertBillingMetadata(ctx, usagerepo.UpsertBillingMetadataParams{
		OrganizationID:         orgID,
		TumMonthlyTokenLimit:   pgtype.Int8{},
		AlertEmail:             conv.ToPGText(recipient),
		BillingCycleAnchorDay:  anchorDay,
		TunneledMcpServerLimit: pgtype.Int4{},
	})
	require.NoError(t, err)

	// Seed usage in the current cycle and at the comparable elapsed point of
	// the previous cycle so the total renders a change badge. The cache read
	// tokens must NOT count toward the totals: they are excluded from the
	// TUM definition.
	now := time.Now().UTC()
	cycles := usage.BillingCycles(now, anchorDay, 2)
	previous, current := cycles[0], cycles[1]
	elapsed := now.Sub(current.Start)

	insertWeeklyUsageComponents(t, ctx, chConn, project.ID.String(), now, 30_204_118, 10_875_321, 4_041_451, 99_999_999)
	insertWeeklyUsageComponents(t, ctx, chConn, project.ID.String(), previous.Start.Add(elapsed/2), 24_910_230, 9_441_006, 3_531_174, 0)

	guardianPolicy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t))
	emails := email.NewService(testenv.NewLogger(t), loops.New(ctx, testenv.NewLogger(t), guardianPolicy, apiKey))
	siteURL, err := url.Parse("https://app.getgram.ai")
	require.NoError(t, err)

	act := activities.NewWeeklyUsageSummary(testenv.NewLogger(t), conn, chConn, emails, siteURL)

	targets, err := act.ListTargets(ctx)
	require.NoError(t, err)
	var target *activities.WeeklyUsageSummaryTarget
	for i := range targets {
		if targets[i].OrganizationID == orgID {
			target = &targets[i]
			break
		}
	}
	require.NotNil(t, target, "seeded org must be resolved as a weekly summary target")
	require.Equal(t, recipient, target.AlertEmail)
	require.Equal(t, anchorDay, target.AnchorDay)

	require.NoError(t, act.Send(ctx, activities.SendWeeklyUsageSummaryArgs{
		Target:  *target,
		RunTime: now,
	}))

	t.Logf("weekly usage summary sent to %s — verify the inbox: cycle-to-date total with change badge, cache reads excluded", recipient)
}
