package risk_exclusion_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities/risk_exclusion"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"go.temporal.io/sdk/testsuite"
)

// runReconcile executes the activity inside a Temporal test activity
// environment so activity.RecordHeartbeat and heartbeat-detail reads work.
func runReconcile(t *testing.T, r *risk_exclusion.Reconcile, args risk_exclusion.ReconcileArgs) error {
	t.Helper()
	var ts testsuite.WorkflowTestSuite
	env := ts.NewTestActivityEnvironment()
	env.SetTestTimeout(5 * time.Minute)
	env.RegisterActivity(r.Do)
	if _, err := env.ExecuteActivity(r.Do, args); err != nil {
		return fmt.Errorf("execute reconcile activity: %w", err)
	}
	return nil
}

// chFinding builds one live ClickHouse finding row for a tenant, two days
// old so the reconcile's retention sweep covers it.
func chFinding(tenant testTenant, ruleID, source string) chrepo.RiskFindingRow {
	createdAt := time.Now().UTC().AddDate(0, 0, -2).Truncate(time.Hour)
	return chrepo.RiskFindingRow{
		ID:                       uuid.Must(uuid.NewV7()),
		CreatedAt:                createdAt,
		OrganizationID:           tenant.orgID,
		ProjectID:                tenant.projectID.String(),
		RequestID:                "",
		ChatMessageID:            uuid.NewString(),
		ContentPartID:            "",
		RiskPolicyID:             "",
		RiskPolicyVersion:        1,
		RuleID:                   ruleID,
		Description:              "",
		Source:                   source,
		Confidence:               1,
		Tags:                     []string{},
		StartPos:                 0,
		EndPos:                   0,
		DeadLetterReason:         "",
		ChatID:                   "",
		UserID:                   "",
		ExternalUserID:           "",
		MessageCreatedAt:         createdAt,
		AssistantID:              "",
		ChatSource:               "",
		Team:                     "",
		UserEmail:                "",
		Category:                 "secrets",
		MatchLen:                 0,
		MatchRedacted:            "",
		FingerprintPepperVersion: "",
		FingerprintGlobalHS256:   "",
		FingerprintTenantHS256:   "",
		ExcludedAt:               nil,
		ExclusionID:              nil,
		FalsePositiveAt:          nil,
		Surface:                  "",
		Field:                    "",
		Path:                     "",
		ToolCallID:               "",
	}
}

func newReconcile(t *testing.T, conn *pgxpool.Pool, ch *chrepo.Queries) *risk_exclusion.Reconcile {
	t.Helper()
	return risk_exclusion.NewReconcile(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		conn,
		ch,
		risk.Fingerprinter{},
		nil,
	)
}

func latestExcludedBy(t *testing.T, ch chrepo.CHTX, id uuid.UUID) string {
	t.Helper()
	rows, err := ch.Query(t.Context(), `
		SELECT coalesce(toString(exclusion_id), '')
		FROM risk_findings WHERE id = ? ORDER BY inserted_at DESC LIMIT 1
	`, id)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	require.True(t, rows.Next())
	var exclusionID string
	require.NoError(t, rows.Scan(&exclusionID))
	return exclusionID
}

// TestReconcile_RuleIDLifecycle drives create -> disable through the real
// activity against Postgres + ClickHouse: apply flags the matching ClickHouse
// rows, and a reconcile after disabling reverses them.
func TestReconcile_RuleIDLifecycle(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := cloneDB(t)
	tenant := seedTenant(t, conn)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	chQueries := chrepo.New(chConn)

	matching := chFinding(tenant, "secret.github_pat", "gitleaks")
	unrelated := chFinding(tenant, "secret.aws_access_key", "gitleaks")
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{matching, unrelated}))
	testenv.FlushClickHouseAsyncInserts(t, chConn)

	pgRepo := riskrepo.New(conn)
	exclusion, err := pgRepo.CreateRiskExclusion(ctx, riskrepo.CreateRiskExclusionParams{
		ProjectID:      tenant.projectID,
		OrganizationID: tenant.orgID,
		RiskPolicyID:   uuid.NullUUID{},
		MatchType:      "rule_id",
		MatchValue:     "secret.github_pat",
		RuleIDFilter:   pgtype.Text{},
		SourceFilter:   pgtype.Text{},
		Enabled:        true,
	})
	require.NoError(t, err)

	reconcile := newReconcile(t, conn, chQueries)
	require.NoError(t, runReconcile(t, reconcile, risk_exclusion.ReconcileArgs{
		ProjectID:   tenant.projectID,
		ExclusionID: exclusion.ID,
		WindowDays:  0,
	}))

	require.Equal(t, exclusion.ID.String(), latestExcludedBy(t, chConn, matching.ID))
	require.Empty(t, latestExcludedBy(t, chConn, unrelated.ID))

	// Disable and reconcile again: the reverse phase un-hides the finding.
	_, err = pgRepo.UpdateRiskExclusion(ctx, riskrepo.UpdateRiskExclusionParams{
		ID:           exclusion.ID,
		ProjectID:    tenant.projectID,
		RiskPolicyID: uuid.NullUUID{},
		MatchType:    exclusion.MatchType,
		MatchValue:   exclusion.MatchValue,
		RuleIDFilter: pgtype.Text{},
		SourceFilter: pgtype.Text{},
		Enabled:      false,
	})
	require.NoError(t, err)

	require.NoError(t, runReconcile(t, reconcile, risk_exclusion.ReconcileArgs{
		ProjectID:   tenant.projectID,
		ExclusionID: exclusion.ID,
		WindowDays:  0,
	}))
	require.Empty(t, latestExcludedBy(t, chConn, matching.ID))
}

// TestReconcile_WindowDaysBoundsClickHouseSweep pins the bounded sweep the
// workflow's delayed second pass uses: a one-day window only touches today's
// partition, leaving older findings for the unbounded first pass.
func TestReconcile_WindowDaysBoundsClickHouseSweep(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := cloneDB(t)
	tenant := seedTenant(t, conn)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	chQueries := chrepo.New(chConn)

	// chFinding writes two days back, outside a one-day window.
	older := chFinding(tenant, "secret.github_pat", "gitleaks")
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{older}))
	testenv.FlushClickHouseAsyncInserts(t, chConn)

	exclusion, err := riskrepo.New(conn).CreateRiskExclusion(ctx, riskrepo.CreateRiskExclusionParams{
		ProjectID:      tenant.projectID,
		OrganizationID: tenant.orgID,
		RiskPolicyID:   uuid.NullUUID{},
		MatchType:      "rule_id",
		MatchValue:     "secret.github_pat",
		RuleIDFilter:   pgtype.Text{},
		SourceFilter:   pgtype.Text{},
		Enabled:        true,
	})
	require.NoError(t, err)

	reconcile := newReconcile(t, conn, chQueries)
	require.NoError(t, runReconcile(t, reconcile, risk_exclusion.ReconcileArgs{
		ProjectID:   tenant.projectID,
		ExclusionID: exclusion.ID,
		WindowDays:  1,
	}))
	require.Empty(t, latestExcludedBy(t, chConn, older.ID), "a windowed sweep never scans older partitions")

	require.NoError(t, runReconcile(t, reconcile, risk_exclusion.ReconcileArgs{
		ProjectID:   tenant.projectID,
		ExclusionID: exclusion.ID,
		WindowDays:  0,
	}))
	require.Equal(t, exclusion.ID.String(), latestExcludedBy(t, chConn, older.ID))
}

// TestReconcile_NilClickHouseDegrades pins the CH-less worker behavior: the
// Postgres phases run and the activity succeeds without touching ClickHouse.
func TestReconcile_NilClickHouseDegrades(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := cloneDB(t)
	tenant := seedTenant(t, conn)

	exclusion, err := riskrepo.New(conn).CreateRiskExclusion(ctx, riskrepo.CreateRiskExclusionParams{
		ProjectID:      tenant.projectID,
		OrganizationID: tenant.orgID,
		RiskPolicyID:   uuid.NullUUID{},
		MatchType:      "rule_id",
		MatchValue:     "secret.github_pat",
		RuleIDFilter:   pgtype.Text{},
		SourceFilter:   pgtype.Text{},
		Enabled:        true,
	})
	require.NoError(t, err)

	reconcile := newReconcile(t, conn, nil)
	require.NoError(t, runReconcile(t, reconcile, risk_exclusion.ReconcileArgs{
		ProjectID:   tenant.projectID,
		ExclusionID: exclusion.ID,
	}))
}

// TestReconcile_ExactWithoutFingerprinterSkipsCH pins the degradation for
// exact-match exclusions when no pepper keyring is configured: the ClickHouse
// apply is skipped (rows stay visible) but the activity still succeeds.
func TestReconcile_ExactWithoutFingerprinterSkipsCH(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := cloneDB(t)
	tenant := seedTenant(t, conn)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	chQueries := chrepo.New(chConn)

	finding := chFinding(tenant, "secret.github_pat", "gitleaks")
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{finding}))
	testenv.FlushClickHouseAsyncInserts(t, chConn)

	exclusion, err := riskrepo.New(conn).CreateRiskExclusion(ctx, riskrepo.CreateRiskExclusionParams{
		ProjectID:      tenant.projectID,
		OrganizationID: tenant.orgID,
		RiskPolicyID:   uuid.NullUUID{},
		MatchType:      "exact",
		MatchValue:     "AKIAIOSFODNN7EXAMPLE",
		RuleIDFilter:   pgtype.Text{},
		SourceFilter:   pgtype.Text{},
		Enabled:        true,
	})
	require.NoError(t, err)

	reconcile := newReconcile(t, conn, chQueries)
	require.NoError(t, runReconcile(t, reconcile, risk_exclusion.ReconcileArgs{
		ProjectID:   tenant.projectID,
		ExclusionID: exclusion.ID,
	}))
	require.Empty(t, latestExcludedBy(t, chConn, finding.ID))
}
