package activities_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

// billingCycleFinalizeGrace mirrors the activity's grace period so tests can
// predict which cycles the activity finalizes.
const billingCycleFinalizeGrace = 72 * time.Hour

func setupSnapshotBillingCycleUsageTest(t *testing.T, dbName string) (act *activities.SnapshotBillingCycleUsage, conn *pgxpool.Pool, telemetryQueries *telemetryrepo.Queries, orgID string, projectID uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	conn, err := infra.CloneTestDatabase(t, dbName)
	require.NoError(t, err)

	chConn, err := infra.NewClickhouseClient(t)
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

	act = activities.NewSnapshotBillingCycleUsage(testenv.NewLogger(t), conn, chConn)

	return act, conn, telemetryrepo.New(chConn), orgID, project.ID
}

// insertTUMTelemetryRow inserts a raw telemetry_logs row. The
// chat_token_summaries materialized view derives the chat id from
// attributes["gen_ai.conversation.id"].
func insertTUMTelemetryRow(t *testing.T, ctx context.Context, queries *telemetryrepo.Queries, projectID string, timestamp time.Time, gramURN string, attributes map[string]any) {
	t.Helper()

	attrsJSON, err := json.Marshal(attributes)
	require.NoError(t, err)

	err = queries.InsertTelemetryLog(ctx, telemetryrepo.InsertTelemetryLogParams{
		ID:                   uuid.NewString(),
		TimeUnixNano:         timestamp.UnixNano(),
		ObservedTimeUnixNano: timestamp.UnixNano(),
		SeverityText:         nil,
		Body:                 "tum snapshot test",
		TraceID:              nil,
		SpanID:               nil,
		Attributes:           string(attrsJSON),
		ResourceAttributes:   "{}",
		GramProjectID:        projectID,
		GramDeploymentID:     nil,
		GramFunctionID:       nil,
		GramURN:              gramURN,
		ServiceName:          "gram-test",
		ServiceVersion:       nil,
		GramChatID:           nil,
	})
	require.NoError(t, err)
}

// insertStoredSession inserts token-usage rows plus a tool-call row for a chat
// so the session qualifies as "stored" and counts toward TUM.
func insertStoredSession(t *testing.T, ctx context.Context, queries *telemetryrepo.Queries, projectID string, timestamp time.Time, totalTokens int) {
	t.Helper()

	chatID := uuid.NewString()
	insertTUMTelemetryRow(t, ctx, queries, projectID, timestamp, "chat:completion", map[string]any{
		"gen_ai.conversation.id":     chatID,
		"gen_ai.usage.input_tokens":  totalTokens / 2,
		"gen_ai.usage.output_tokens": totalTokens - totalTokens/2,
		"gen_ai.usage.total_tokens":  totalTokens,
		"gram.resource.urn":          "chat:completion",
	})
	insertTUMTelemetryRow(t, ctx, queries, projectID, timestamp, "tools:http:petstore:listPets", map[string]any{
		"gen_ai.conversation.id":    chatID,
		"gram.tool.urn":             "tools:http:petstore:listPets",
		"http.response.status_code": 200,
	})
}

func TestSnapshotBillingCycleUsage_SnapshotsTrailingCycles(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	act, conn, telemetryQueries, orgID, projectID := setupSnapshotBillingCycleUsageTest(t, "snapshot_billing_cycles")
	queries := usagerepo.New(conn)

	insertStoredSession(t, ctx, telemetryQueries, projectID.String(), time.Now().UTC(), 450)

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

	act, conn, telemetryQueries, orgID, projectID := setupSnapshotBillingCycleUsageTest(t, "snapshot_billing_deleted_project")
	queries := usagerepo.New(conn)

	insertStoredSession(t, ctx, telemetryQueries, projectID.String(), time.Now().UTC(), 300)

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

	act := activities.NewSnapshotBillingCycleUsage(testenv.NewLogger(t), conn, chConn)
	require.NoError(t, act.Do(ctx, []string{orgID}))

	rows, err := usagerepo.New(conn).ListBillingCycleUsage(ctx, orgID)
	require.NoError(t, err)
	require.Empty(t, rows, "orgs without projects should not accumulate snapshots")
}
