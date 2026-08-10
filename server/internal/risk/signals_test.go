package risk_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestGetRiskSignals_ClickHouse seeds ClickHouse findings across the current
// window, the previous comparison window, and the trailing 24h KPI windows,
// plus rows every signal query must ignore (redelivered duplicate id,
// excluded, false positive, dead letter, foreign tenant), and asserts the
// clustered signals, KPI splits, and exposure rollup.
func TestGetRiskSignals_ClickHouse(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -30).Truncate(24 * time.Hour)
	to := from.AddDate(0, 0, 7)

	chatA := uuid.Must(uuid.NewV7())
	chatB := uuid.Must(uuid.NewV7())
	msg := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }

	// Current window: three secret findings (alice x2, bob x1; one inside the
	// trailing 24h), three pii findings (alice; one inside the previous-24h KPI
	// bucket at to-30h).
	rows := []chrepo.RiskFindingRow{
		chOverviewFinding(t, projectID, orgID, chatA, msg(), from.Add(36*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com"),
		chOverviewFinding(t, projectID, orgID, chatB, msg(), from.Add(38*time.Hour), "gitleaks", "secret.github_pat", "bob@example.com"),
		chOverviewFinding(t, projectID, orgID, chatA, msg(), to.Add(-2*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com"),
		chOverviewFinding(t, projectID, orgID, chatA, msg(), from.Add(60*time.Hour), "presidio", "pii.email_address", "alice@example.com"),
		chOverviewFinding(t, projectID, orgID, chatA, msg(), from.Add(61*time.Hour), "presidio", "pii.email_address", "alice@example.com"),
		chOverviewFinding(t, projectID, orgID, chatA, msg(), to.Add(-30*time.Hour), "presidio", "pii.email_address", "alice@example.com"),
		// Previous window: one secret finding (alice) -> secret signal trends
		// 3x, previous users exposed = 1.
		chOverviewFinding(t, projectID, orgID, chatA, msg(), from.Add(-24*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com"),
	}

	// App/team/email attribution on the secret findings only: apps aggregate
	// to the distinct chat sources, teams to the distinct non-empty team
	// names, and the top-users rows carry the per-user team. Bob's stamped
	// user_email must take precedence over his @-shaped external id; alice
	// (no stamp) falls back to her external id.
	rows[0].ChatSource = "codex"
	rows[0].Team = "Platform"
	rows[1].ChatSource = "cursor"
	rows[1].UserEmail = "robert@corp.example"
	rows[2].Team = "Platform"

	// Redelivered duplicate id: counted once by every uniqExact.
	rows = append(rows, rows[0])

	// Excluded at ingest: invisible to signals.
	excludedAt := from.Add(40 * time.Hour)
	excludedRow := chOverviewFinding(t, projectID, orgID, chatA, msg(), from.Add(40*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com")
	excludedRow.ExcludedAt = &excludedAt
	exclusionID := uuid.Must(uuid.NewV7())
	excludedRow.ExclusionID = &exclusionID
	rows = append(rows, excludedRow)

	// Dead-letter sentinel: invisible to signals.
	deadLetterRow := chOverviewFinding(t, projectID, orgID, chatA, msg(), from.Add(41*time.Hour), "gitleaks", "", "alice@example.com")
	deadLetterRow.DeadLetterReason = "could-not-analyze"
	deadLetterRow.Category = ""
	rows = append(rows, deadLetterRow)

	// Foreign tenant: invisible to signals.
	rows = append(rows, chOverviewFinding(t, uuid.New(), "org_"+uuid.NewString(), chatA, msg(), from.Add(42*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com"))

	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, rows))

	// Post-hoc false positive: a bare row with false_positive_at set must not
	// count anywhere.
	require.NoError(t, ti.chConn.Exec(ctx, `
		INSERT INTO risk_findings (id, created_at, message_created_at, organization_id, project_id, rule_id, source, category, chat_id, false_positive_at)
		VALUES (?, ?, ?, ?, ?, 'secret.github_pat', 'gitleaks', 'secrets', ?, ?)
	`, uuid.Must(uuid.NewV7()), from.Add(43*time.Hour), from.Add(43*time.Hour), orgID, projectID.String(), chatA.String(), from.Add(44*time.Hour)))

	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.GetRiskSignals(ctx, &gen.GetRiskSignalsPayload{
		From: new(from.Format(time.RFC3339)),
		To:   new(to.Format(time.RFC3339)),
	})
	require.NoError(t, err)

	require.Equal(t, from.Format(time.RFC3339), result.From)
	require.Equal(t, to.Format(time.RFC3339), result.To)

	require.Len(t, result.Signals, 2)

	secrets := result.Signals[0]
	require.Equal(t, "rule:secret.github_pat", secrets.Key)
	require.Equal(t, "secret.github_pat", secrets.RuleID)
	require.Equal(t, "secrets", secrets.Category)
	require.Equal(t, []string{"gitleaks"}, secrets.DetectionSources)
	require.Equal(t, []string{"codex", "cursor"}, secrets.Apps)
	require.Equal(t, int64(3), secrets.Findings)
	require.Equal(t, int64(1), secrets.PreviousFindings)
	require.Equal(t, int64(2), secrets.Users)
	require.Equal(t, int64(1), secrets.Teams)
	require.Equal(t, "critical", secrets.Severity)
	require.InDelta(t, 9.9, secrets.RiskScore, 0.001)
	require.Equal(t, from.Add(36*time.Hour).Format(time.RFC3339), secrets.FirstSeen)
	require.Equal(t, to.Add(-2*time.Hour).Format(time.RFC3339), secrets.LastSeen)
	// Sparkline covers the window and sums to the deduplicated finding count;
	// only the current-window findings contribute (the previous-window row and
	// the filtered rows must not).
	require.NotEmpty(t, secrets.Sparkline)
	var sparkTotal int64
	for _, bucket := range secrets.Sparkline {
		sparkTotal += bucket
	}
	require.Equal(t, secrets.Findings, sparkTotal)
	require.Len(t, secrets.TopUsers, 2)
	require.Equal(t, "alice@example.com", secrets.TopUsers[0].Email)
	require.Equal(t, int64(2), secrets.TopUsers[0].Findings)
	require.Equal(t, "Platform", secrets.TopUsers[0].Team)
	require.Equal(t, "robert@corp.example", secrets.TopUsers[1].Email)
	require.Equal(t, int64(1), secrets.TopUsers[1].Findings)
	require.Empty(t, secrets.TopUsers[1].Team)

	pii := result.Signals[1]
	require.Equal(t, "pii.email_address", pii.RuleID)
	require.Equal(t, "pii", pii.Category)
	require.Empty(t, pii.Apps)
	require.Equal(t, int64(0), pii.Teams)
	require.Equal(t, int64(3), pii.Findings)
	require.Equal(t, int64(0), pii.PreviousFindings)
	require.Equal(t, int64(1), pii.Users)
	require.Equal(t, "medium", pii.Severity)
	require.Len(t, pii.TopUsers, 1)
	require.Equal(t, "alice@example.com", pii.TopUsers[0].Email)
	require.Equal(t, int64(3), pii.TopUsers[0].Findings)

	require.Equal(t, int64(2), result.OpenSignals)
	require.Equal(t, int64(1), result.CriticalSignals)
	require.Equal(t, int64(2), result.UsersExposed)
	require.Equal(t, int64(1), result.PreviousUsersExposed)
	require.Equal(t, int64(1), result.Findings24h)
	require.Equal(t, int64(1), result.PreviousFindings24h)

	require.Greater(t, result.OrgRiskScore, 0.0)
	require.LessOrEqual(t, result.OrgRiskScore, 10.0)
	require.Greater(t, result.PreviousOrgRiskScore, 0.0)

	// Exposure: 3 secrets + 3 pii, equal counts tie-break by category name.
	require.Len(t, result.Exposure, 2)
	require.Equal(t, "pii", result.Exposure[0].Category)
	require.Equal(t, int64(3), result.Exposure[0].Findings)
	require.InDelta(t, 0.5, result.Exposure[0].Share, 0.001)
	require.Equal(t, "secrets", result.Exposure[1].Category)
	require.InDelta(t, 0.5, result.Exposure[1].Share, 0.001)
}

// TestGetRiskSignals_EmptyWindow asserts an org with no findings gets a clean
// zero result rather than an error.
func TestGetRiskSignals_EmptyWindow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	result, err := ti.service.GetRiskSignals(ctx, &gen.GetRiskSignalsPayload{From: nil, To: nil})
	require.NoError(t, err)

	require.Empty(t, result.Signals)
	require.Empty(t, result.Exposure)
	require.Equal(t, int64(0), result.OpenSignals)
	require.Equal(t, int64(0), result.CriticalSignals)
	require.Equal(t, int64(0), result.UsersExposed)
	require.Equal(t, int64(0), result.Findings24h)
	require.InDelta(t, 0, result.OrgRiskScore, 0.001)
	require.InDelta(t, 0, result.PreviousOrgRiskScore, 0.001)
}

// TestGetRiskSignals_RequiresOrgAdmin asserts the endpoint denies callers
// without the org:admin scope.
func TestGetRiskSignals_RequiresOrgAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.GetRiskSignals(ctx, &gen.GetRiskSignalsPayload{From: nil, To: nil})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

// TestGetRiskSignals_InvalidWindow asserts window validation mirrors the
// overview endpoint (from before to, 31-day cap).
func TestGetRiskSignals_InvalidWindow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	now := time.Now().UTC()
	_, err := ti.service.GetRiskSignals(ctx, &gen.GetRiskSignalsPayload{
		From: new(now.Format(time.RFC3339)),
		To:   new(now.Add(-time.Hour).Format(time.RFC3339)),
	})
	require.Error(t, err)

	_, err = ti.service.GetRiskSignals(ctx, &gen.GetRiskSignalsPayload{
		From: new(now.AddDate(0, 0, -40).Format(time.RFC3339)),
		To:   new(now.Format(time.RFC3339)),
	})
	require.Error(t, err)
}
