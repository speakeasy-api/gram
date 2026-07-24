package activities_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/email"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

// billingCycleFinalizeGrace mirrors the activity's grace period so tests can
// predict which cycles the activity finalizes.
const billingCycleFinalizeGrace = 72 * time.Hour

// captureLoopsClient records every transactional email the activity attempts
// to send so tests can assert on the exact payloads.
type captureLoopsClient struct {
	mu   sync.Mutex
	sent []loops.SendTransactionalInput
	// failNext makes the next SendTransactional calls fail without recording,
	// simulating a transport outage.
	failNext int
}

func (c *captureLoopsClient) SendTransactional(_ context.Context, input loops.SendTransactionalInput) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failNext > 0 {
		c.failNext--
		return errors.New("loops unavailable")
	}
	c.sent = append(c.sent, input)
	return nil
}

func (c *captureLoopsClient) Sent() []loops.SendTransactionalInput {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]loops.SendTransactionalInput, len(c.sent))
	copy(out, c.sent)
	return out
}

func (c *captureLoopsClient) FailNext(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failNext = n
}

func setupSnapshotBillingCycleUsageTest(t *testing.T, dbName string) (act *activities.SnapshotBillingCycleUsage, conn *pgxpool.Pool, chConn clickhouse.Conn, orgID string, projectID uuid.UUID) {
	t.Helper()
	act, conn, chConn, orgID, projectID, _ = setupSnapshotBillingCycleUsageTestWithEmail(t, dbName)
	return act, conn, chConn, orgID, projectID
}

func setupSnapshotBillingCycleUsageTestWithEmail(t *testing.T, dbName string) (act *activities.SnapshotBillingCycleUsage, conn *pgxpool.Pool, chConn clickhouse.Conn, orgID string, projectID uuid.UUID, captured *captureLoopsClient) {
	t.Helper()
	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, dbName)
	require.NoError(t, err)

	chConn, err = infra.NewClickhouseClient(t)
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	orgID = "org-" + uuid.NewString()[:8]
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Test Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "Test Project",
		Slug:           "proj-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	captured = &captureLoopsClient{sent: nil, failNext: 0}
	act = activities.NewSnapshotBillingCycleUsage(
		testenv.NewLogger(t),
		conn,
		chConn,
		cache.NewRedisCacheAdapter(redisClient),
		email.NewService(testenv.NewLogger(t), captured),
	)

	return act, conn, chConn, orgID, project.ID, captured
}

// insertStoredSession seeds an observed Claude Code session directly into the
// tokens-under-management population (attribute_metrics_summaries), with the
// tokens landing as input so the TUM measure counts exactly totalTokens.
//
// It writes the aggregate row directly rather than a raw telemetry_logs row
// routed through attribute_metrics_summaries_mv: the MV only admits event time
// >= its ingestion cutoff (2026-07-14 00:00:00 UTC; see
// server/clickhouse/schema.sql), but these tests read the *active billing
// cycle* anchored to the real wall clock. Seeding directly at timestamp (real
// now) keeps the row inside the active cycle regardless of where the cutoff
// sits. generation 0 / is_active 1 match a live MV row.
func insertStoredSession(t *testing.T, ctx context.Context, chConn clickhouse.Conn, projectID string, timestamp time.Time, totalTokens int) {
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
			generation, is_active, hook_hostname
		)
		SELECT
			toUUID(?) AS gram_project_id,
			toStartOfHour(fromUnixTimestamp64Nano(?)) AS time_bucket,
			'' AS department_name, '' AS job_title, '' AS employee_type,
			'' AS division_name, '' AS cost_center_name,
			'' AS user_email, 'claude-4.6' AS model, 'claude-code' AS hook_source,
			[]::Array(String) AS roles, []::Array(String) AS groups,
			uniqExactIfState(toString('stored-session'), toUInt8(1)) AS total_chats,
			sumIfState(toInt64(?), toUInt8(1)) AS total_input_tokens,
			sumIfState(toInt64(0), toUInt8(1)) AS total_output_tokens,
			sumIfState(toInt64(?), toUInt8(1)) AS total_tokens,
			sumIfState(toInt64(0), toUInt8(1)) AS cache_read_input_tokens,
			sumIfState(toInt64(0), toUInt8(1)) AS cache_creation_input_tokens,
			sumIfState(toFloat64(0), toUInt8(1)) AS total_cost,
			countIfState(toUInt8(0)) AS total_tool_calls,
			uniqExactIfState(toString(''), toUInt8(0)) AS unique_tool_calls,
			'' AS account_type, '' AS provider, '' AS billing_mode,
			'' AS query_source, '' AS skill_name, '' AS agent_name,
			'' AS mcp_server_name, '' AS mcp_tool_name,
			toUInt8(0) AS generation, toUInt8(1) AS is_active,
			'' AS hook_hostname
	`, projectID, timestamp.UnixNano(), totalTokens, totalTokens)
	require.NoError(t, err)
}

func TestSnapshotBillingCycleUsage_SnapshotsTrailingCycles(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, chConn, orgID, projectID := setupSnapshotBillingCycleUsageTest(t, "snapshot_billing_cycles")
	queries := usagerepo.New(conn)

	insertStoredSession(t, ctx, chConn, projectID.String(), time.Now().UTC(), 450)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		if !assert.NoError(c, act.Do(ctx, []string{orgID})) {
			return
		}

		rows, err := queries.ListBillingCycleUsage(ctx, orgID)
		if !assert.NoError(c, err) {
			return
		}
		if !assert.Len(c, rows, 12, "one snapshot per trailing billing cycle") {
			return
		}

		// Cycles are contiguous and ordered.
		for i := 1; i < len(rows); i++ {
			assert.Equal(c, rows[i-1].CycleEnd.Time, rows[i].CycleStart.Time, "cycles should be contiguous")
		}

		now := time.Now().UTC()
		active := rows[len(rows)-1]
		assert.Equal(c, int64(450), active.TumTokens, "active cycle should carry the stored session tokens")
		assert.False(c, active.FinalizedAt.Valid, "active cycle must not be finalized")
		assert.True(c, active.CycleEnd.Time.After(now), "active cycle end is in the future")

		for _, row := range rows[:len(rows)-1] {
			assert.Equal(c, int64(0), row.TumTokens)
			if now.After(row.CycleEnd.Time.Add(billingCycleFinalizeGrace)) {
				assert.True(c, row.FinalizedAt.Valid, "cycles past the grace period should be finalized")
			}
		}
	}, 15*time.Second, 500*time.Millisecond)
}

func TestSnapshotBillingCycleUsage_FinalizedRowsImmutable(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, _, orgID, _ := setupSnapshotBillingCycleUsageTest(t, "snapshot_billing_finalized")
	queries := usagerepo.New(conn)

	// No ClickHouse data: every cycle snapshots as zero and old cycles finalize.
	require.NoError(t, act.Do(ctx, []string{orgID}))

	rows, err := queries.ListBillingCycleUsage(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, rows, 12)

	oldest := rows[0]
	require.True(t, oldest.FinalizedAt.Valid, "oldest cycle should be finalized")

	// A later refresh must not be able to rewrite a finalized cycle.
	err = queries.UpsertBillingCycleUsage(ctx, usagerepo.UpsertBillingCycleUsageParams{
		OrganizationID: orgID,
		CycleStart:     oldest.CycleStart,
		CycleEnd:       oldest.CycleEnd,
		TumTokens:      999,
		FinalizedAt:    pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
	})
	require.NoError(t, err)

	rows, err = queries.ListBillingCycleUsage(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, int64(0), rows[0].TumTokens, "finalized snapshot must be immutable")
	require.True(t, rows[0].FinalizedAt.Valid, "finalization must not be cleared")

	// The active (non-finalized) cycle stays refreshable.
	active := rows[len(rows)-1]
	require.False(t, active.FinalizedAt.Valid)
	err = queries.UpsertBillingCycleUsage(ctx, usagerepo.UpsertBillingCycleUsageParams{
		OrganizationID: orgID,
		CycleStart:     active.CycleStart,
		CycleEnd:       active.CycleEnd,
		TumTokens:      111,
		FinalizedAt:    pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
	})
	require.NoError(t, err)

	rows, err = queries.ListBillingCycleUsage(ctx, orgID)
	require.NoError(t, err)
	require.Equal(t, int64(111), rows[len(rows)-1].TumTokens)
}

func TestSnapshotBillingCycleUsage_RejectsNegativeTokens(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, _, orgID, _ := setupSnapshotBillingCycleUsageTest(t, "snapshot_billing_negative")
	queries := usagerepo.New(conn)

	require.NoError(t, act.Do(ctx, []string{orgID}))

	rows, err := queries.ListBillingCycleUsage(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, rows, 12)

	// Token counts derive from client-supplied OTEL attributes; the permanent
	// billing record must refuse a negative sum outright.
	active := rows[len(rows)-1]
	err = queries.UpsertBillingCycleUsage(ctx, usagerepo.UpsertBillingCycleUsageParams{
		OrganizationID: orgID,
		CycleStart:     active.CycleStart,
		CycleEnd:       active.CycleEnd,
		TumTokens:      -1,
		FinalizedAt:    pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
	})
	require.ErrorContains(t, err, "billing_cycle_usage_tum_tokens_check")
}

func TestSnapshotBillingCycleUsage_IncludesDeletedProjects(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, chConn, orgID, projectID := setupSnapshotBillingCycleUsageTest(t, "snapshot_billing_deleted_project")
	queries := usagerepo.New(conn)

	insertStoredSession(t, ctx, chConn, projectID.String(), time.Now().UTC(), 300)

	// Deleting the project mid-cycle must not erase its usage from the
	// billing record: the tokens were consumed while it was live.
	_, err := projectsrepo.New(conn).DeleteProject(ctx, projectID)
	require.NoError(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		if !assert.NoError(c, act.Do(ctx, []string{orgID})) {
			return
		}

		rows, err := queries.ListBillingCycleUsage(ctx, orgID)
		if !assert.NoError(c, err) {
			return
		}
		if !assert.Len(c, rows, 12) {
			return
		}

		active := rows[len(rows)-1]
		assert.Equal(c, int64(300), active.TumTokens, "deleted project's usage must still count toward the cycle")
	}, 15*time.Second, 500*time.Millisecond)
}

func TestSnapshotBillingCycleUsage_NoProjects(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, "snapshot_billing_no_projects")
	require.NoError(t, err)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)

	orgID := "org-" + uuid.NewString()[:8]
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Test Org",
		Slug:        orgID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)

	act := activities.NewSnapshotBillingCycleUsage(
		testenv.NewLogger(t),
		conn,
		chConn,
		cache.NewRedisCacheAdapter(redisClient),
		email.NewService(testenv.NewLogger(t), &captureLoopsClient{sent: nil, failNext: 0}),
	)
	require.NoError(t, act.Do(ctx, []string{orgID}))

	rows, err := usagerepo.New(conn).ListBillingCycleUsage(ctx, orgID)
	require.NoError(t, err)
	require.Empty(t, rows, "orgs without projects should not accumulate snapshots")
}
