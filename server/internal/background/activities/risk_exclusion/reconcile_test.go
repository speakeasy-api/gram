package risk_exclusion_test

import (
	"fmt"
	"strings"
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
// exact-match exclusions when no pepper keyring is configured: BOTH ClickHouse
// phases are skipped — unmatched rows stay visible, and rows the exclusion
// already held (annotated at ingest) stay hidden, because a reversal whose
// apply cannot run would permanently expose them. The activity still succeeds.
func TestReconcile_ExactWithoutFingerprinterSkipsCH(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := cloneDB(t)
	tenant := seedTenant(t, conn)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	chQueries := chrepo.New(chConn)

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

	visible := chFinding(tenant, "secret.github_pat", "gitleaks")
	heldAtIngest := chFinding(tenant, "secret.github_pat", "gitleaks")
	heldAt := time.Now().UTC().Add(-time.Hour)
	heldAtIngest.ExcludedAt = &heldAt
	heldAtIngest.ExclusionID = &exclusion.ID
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{visible, heldAtIngest}))
	testenv.FlushClickHouseAsyncInserts(t, chConn)

	reconcile := newReconcile(t, conn, chQueries)
	require.NoError(t, runReconcile(t, reconcile, risk_exclusion.ReconcileArgs{
		ProjectID:   tenant.projectID,
		ExclusionID: exclusion.ID,
	}))
	require.Empty(t, latestExcludedBy(t, chConn, visible.ID), "the apply cannot run without fingerprints")
	require.Equal(t, exclusion.ID.String(), latestExcludedBy(t, chConn, heldAtIngest.ID),
		"held rows must not be exposed by a reversal whose apply cannot restore them")
}

// TestReconcile_RegexReversalNeverExposesUnprovableRows drives an active
// regex exclusion through the activity and pins the reversal semantics per
// held row: reconstructable-and-still-matching stays held,
// reconstructable-and-non-matching reverses, and a row whose plaintext cannot
// be reconstructed (its anchoring chat message is gone) stays held. A live
// matching row is flagged by the apply in the same run.
func TestReconcile_RegexReversalNeverExposesUnprovableRows(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn := cloneDB(t)
	tenant := seedTenant(t, conn)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	chQueries := chrepo.New(chConn)
	pgRepo := riskrepo.New(conn)

	secret := "AKIAIOSFODNN7EXAMPLE"
	otherToken := "ghp_0123456789abcdefghijklmnopqrstuvwxyz"

	chatID, err := pgRepo.CreateChatForTest(ctx, riskrepo.CreateChatForTestParams{
		ProjectID: tenant.projectID, OrganizationID: tenant.orgID, UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
	})
	require.NoError(t, err)
	anchored := func(content string) uuid.UUID {
		id, merr := pgRepo.CreateChatMessageForTest(ctx, riskrepo.CreateChatMessageForTestParams{
			ChatID: chatID, ProjectID: uuid.NullUUID{UUID: tenant.projectID, Valid: true}, Content: content,
			UserID: pgtype.Text{}, ExternalUserID: pgtype.Text{},
		})
		require.NoError(t, merr)
		return id
	}

	regexRow := func(msgID uuid.UUID, content, match string) chrepo.RiskFindingRow {
		row := chFinding(tenant, "secret.aws_access_key", "gitleaks")
		row.ChatMessageID = msgID.String()
		row.ChatID = chatID.String()
		start := int32(strings.Index(content, match))
		row.StartPos = start
		row.EndPos = start + int32(len(match))
		row.MatchLen = uint32(len(match))
		row.Surface = "content"
		return row
	}

	exclusion, err := pgRepo.CreateRiskExclusion(ctx, riskrepo.CreateRiskExclusionParams{
		ProjectID:      tenant.projectID,
		OrganizationID: tenant.orgID,
		RiskPolicyID:   uuid.NullUUID{},
		MatchType:      "regex",
		MatchValue:     "AKIA[0-9A-Z]{16}",
		RuleIDFilter:   pgtype.Text{},
		SourceFilter:   pgtype.Text{},
		Enabled:        true,
	})
	require.NoError(t, err)

	heldAt := time.Now().UTC().Add(-time.Hour)
	hold := func(row chrepo.RiskFindingRow) chrepo.RiskFindingRow {
		row.ExcludedAt = &heldAt
		row.ExclusionID = &exclusion.ID
		return row
	}

	matchingContent := "please rotate " + secret + " before the audit"
	staleContent := "the token " + otherToken + " was revoked"

	heldMatching := hold(regexRow(anchored(matchingContent), matchingContent, secret))
	heldStale := hold(regexRow(anchored(staleContent), staleContent, otherToken))
	heldOrphan := hold(regexRow(uuid.Must(uuid.NewV7()), matchingContent, secret))
	liveMatching := regexRow(anchored(matchingContent), matchingContent, secret)

	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{
		heldMatching, heldStale, heldOrphan, liveMatching,
	}))
	testenv.FlushClickHouseAsyncInserts(t, chConn)

	reconcile := newReconcile(t, conn, chQueries)
	require.NoError(t, runReconcile(t, reconcile, risk_exclusion.ReconcileArgs{
		ProjectID:   tenant.projectID,
		ExclusionID: exclusion.ID,
	}))

	require.Equal(t, exclusion.ID.String(), latestExcludedBy(t, chConn, heldMatching.ID),
		"a held row whose plaintext still matches stays held")
	require.Empty(t, latestExcludedBy(t, chConn, heldStale.ID),
		"a held row whose plaintext provably no longer matches reverses")
	require.Equal(t, exclusion.ID.String(), latestExcludedBy(t, chConn, heldOrphan.ID),
		"a held row whose plaintext cannot be reconstructed is never exposed")
	require.Equal(t, exclusion.ID.String(), latestExcludedBy(t, chConn, liveMatching.ID),
		"the apply flags live matching rows in the same run")
}
