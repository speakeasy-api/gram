package risk_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/chrepo"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// chOverviewFinding mirrors what the enriched FindingCHWriter writes for one
// seeded overview finding: category from the shared classifier, chat-level
// attribution (seedChatWithUser chats carry only an external user id).
func chOverviewFinding(t *testing.T, projectID uuid.UUID, orgID string, chatID, msgID uuid.UUID, createdAt time.Time, source, ruleID, externalUserID string) chrepo.RiskFindingRow {
	t.Helper()

	return chrepo.RiskFindingRow{
		ID:                       uuid.Must(uuid.NewV7()),
		CreatedAt:                createdAt,
		OrganizationID:           orgID,
		ProjectID:                projectID.String(),
		RequestID:                "",
		ChatMessageID:            msgID.String(),
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
		ChatID:                   chatID.String(),
		UserID:                   "",
		ExternalUserID:           externalUserID,
		Category:                 string(categories.Classify(source, ruleID)),
		MatchLen:                 0,
		MatchRedacted:            "",
		FingerprintPepperVersion: "",
		FingerprintGlobalHS256:   "",
		FingerprintTenantHS256:   "",
		ExcludedAt:               nil,
		ExclusionID:              nil,
		MessageCreatedAt:         createdAt,
		AssistantID:              "",
	}
}

// TestGetRiskOverview_CustomWindowAggregates seeds ClickHouse findings and
// asserts the overview aggregates. Extra rows probe the read-path filters: a
// redelivered duplicate id, an insert-time-excluded row, a dead-letter
// sentinel, a post-hoc false positive, and a foreign-tenant row must all leave
// the numbers untouched.
func TestGetRiskOverview_CustomWindowAggregates(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -30).Truncate(24 * time.Hour) // 30 days ago, well within ClickHouse TTL
	to := from.AddDate(0, 0, 7)                             // 7-day window

	aliceSecret1Chat, aliceSecret1 := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")
	aliceSecret2Chat, aliceSecret2 := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")
	alicePIIChat, alicePII := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")
	bobShadowChat, bobShadow := seedChatWithUser(t, ti, projectID, orgID, "bob@example.com")
	opaqueFindingChat, opaqueFinding := seedChatWithUser(t, ti, projectID, orgID, "opaque-user-id")
	outsideWindowChat, outsideWindow := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")
	disabledPolicyChat, disabledPolicyFinding := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")

	// The findings as the enriched writer stores them.
	rows := []chrepo.RiskFindingRow{
		chOverviewFinding(t, projectID, orgID, aliceSecret1Chat, aliceSecret1, from.Add(36*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com"),
		chOverviewFinding(t, projectID, orgID, aliceSecret2Chat, aliceSecret2, from.Add(38*time.Hour), "gitleaks", "secret.aws_access_token", "alice@example.com"),
		chOverviewFinding(t, projectID, orgID, alicePIIChat, alicePII, from.Add(60*time.Hour), "presidio", "pii.email_address", "alice@example.com"),
		chOverviewFinding(t, projectID, orgID, bobShadowChat, bobShadow, from.Add(84*time.Hour), "shadow_mcp", "", "bob@example.com"),
		chOverviewFinding(t, projectID, orgID, opaqueFindingChat, opaqueFinding, from.Add(109*time.Hour), "gitleaks", "secret.github_pat", "opaque-user-id"),
		chOverviewFinding(t, projectID, orgID, outsideWindowChat, outsideWindow, to.Add(24*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com"),
		chOverviewFinding(t, projectID, orgID, disabledPolicyChat, disabledPolicyFinding, from.Add(110*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com"),
	}

	// Redelivered duplicate: same row id inserted twice; uniqExact must count
	// it once.
	rows = append(rows, rows[0])

	// Insert-time-excluded row and a dead-letter sentinel: filtered by every
	// overview query.
	excludedAt := from.Add(37 * time.Hour)
	excludedRow := chOverviewFinding(t, projectID, orgID, aliceSecret1Chat, aliceSecret1, from.Add(37*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com")
	excludedRow.ExcludedAt = &excludedAt
	exclusionID := uuid.Must(uuid.NewV7())
	excludedRow.ExclusionID = &exclusionID
	rows = append(rows, excludedRow)

	deadLetterRow := chOverviewFinding(t, projectID, orgID, aliceSecret1Chat, aliceSecret1, from.Add(37*time.Hour), "gitleaks", "", "alice@example.com")
	deadLetterRow.DeadLetterReason = "could-not-analyze"
	deadLetterRow.Category = ""
	rows = append(rows, deadLetterRow)

	// Foreign tenant: same window, different org/project.
	foreignRow := chOverviewFinding(t, uuid.New(), "org_"+uuid.NewString(), aliceSecret1Chat, aliceSecret1, from.Add(37*time.Hour), "gitleaks", "secret.github_pat", "alice@example.com")
	rows = append(rows, foreignRow)

	// Shadow engine-comparison row (the LLM analyzer's verdict recorded under
	// the shadow mode for the same message): stored, never counted. A distinct
	// user so a leak would show up in the user counts as well.
	shadowRow := chOverviewFinding(t, projectID, orgID, aliceSecret1Chat, aliceSecret1, from.Add(37*time.Hour), "gitleaks", "secret.github_pat", "carol@example.com")
	shadowRow.Shadow = true
	rows = append(rows, shadowRow)

	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, rows))

	// Post-hoc false positive: inserted directly with false_positive_at set
	// (the live writer never stamps it; the mark path mirrors it later).
	require.NoError(t, ti.chConn.Exec(ctx, `
		INSERT INTO risk_findings (id, created_at, organization_id, project_id, rule_id, source, category, chat_id, false_positive_at)
		VALUES (?, ?, ?, ?, 'secret.github_pat', 'gitleaks', 'secrets', ?, ?)
	`, uuid.Must(uuid.NewV7()), from.Add(37*time.Hour), orgID, projectID.String(), aliceSecret1Chat.String(), from.Add(40*time.Hour)))

	result, err := ti.service.GetRiskOverview(ctx, &gen.GetRiskOverviewPayload{
		From: new(from.Format(time.RFC3339)),
		To:   new(to.Format(time.RFC3339)),
	})
	require.NoError(t, err)

	require.Equal(t, int64(6), result.Findings)
	require.Equal(t, int64(6), result.FlaggedSessions)

	categoryCounts := map[string]int64{}
	for _, category := range result.TopCategories {
		categoryCounts[category.Category] = category.Findings
	}
	require.Equal(t, int64(4), categoryCounts["secrets"])
	require.Equal(t, int64(1), categoryCounts["pii"])
	require.Equal(t, int64(1), categoryCounts["shadow_mcp"])

	users := map[string]int64{}
	for _, user := range result.TopUsers {
		users[user.Email] = user.Findings
	}
	require.Equal(t, int64(4), users["alice@example.com"])
	require.Equal(t, int64(1), users["bob@example.com"])
	require.Equal(t, int64(1), users["Unknown user"])
	require.NotContains(t, users, "opaque-user-id")
	require.NotContains(t, users, "carol@example.com", "the shadow row never reaches the per-user counts")

	require.Len(t, result.TimeSeriesFindings, 504)
	timeSeries := map[string]int64{}
	for _, point := range result.TimeSeriesFindings {
		timeSeries[point.Category+"|"+point.BucketStart] = point.Findings
	}
	bucket := func(offset time.Duration) string {
		return from.Add(offset).Truncate(time.Hour).Format(time.RFC3339)
	}
	require.Equal(t, int64(1), timeSeries["secrets|"+bucket(36*time.Hour)])
	require.Equal(t, int64(1), timeSeries["secrets|"+bucket(38*time.Hour)])
	require.Equal(t, int64(1), timeSeries["pii|"+bucket(60*time.Hour)])
	require.Equal(t, int64(1), timeSeries["shadow_mcp|"+bucket(84*time.Hour)])
	require.Equal(t, int64(1), timeSeries["secrets|"+bucket(109*time.Hour)])
	require.Equal(t, int64(1), timeSeries["secrets|"+bucket(110*time.Hour)])
	require.Equal(t, int64(0), timeSeries["pii|"+bucket(109*time.Hour)])
}

// TestGetRiskOverview_ClickHouseDedupesAppendedDismissRow covers a finding's
// original row (false_positive_at NULL) plus a later direct dismissal copy
// sharing the same id. A naive "row satisfies false_positive_at IS NULL"
// filter would still count the id via the stale original row; only picking
// each id's most-recently-inserted row before filtering excludes it.
func TestGetRiskOverview_ClickHouseDedupesAppendedDismissRow(t *testing.T) {
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
	to := from.AddDate(0, 0, 1)

	chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, "alice@example.com")

	// A second, untouched finding so the assertion distinguishes "dedup works"
	// from "nothing was counted at all".
	live := chOverviewFinding(t, projectID, orgID, chatID, msgID, from.Add(2*time.Hour), "gitleaks", "secret.aws_access_token", "alice@example.com")
	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{live}))

	// The dismissed finding's original row and its later append, sharing one
	// id. inserted_at is set explicitly (rather than left to the now64(9)
	// default) so the append is unambiguously the latest row regardless of how
	// fast the two INSERTs run.
	dismissedID := uuid.Must(uuid.NewV7())
	require.NoError(t, ti.chConn.Exec(ctx, `
		INSERT INTO risk_findings (id, created_at, inserted_at, organization_id, project_id, rule_id, source, category, chat_id)
		VALUES (?, ?, ?, ?, ?, 'secret.github_pat', 'gitleaks', 'secrets', ?)
	`, dismissedID, from.Add(3*time.Hour), from.Add(3*time.Hour), orgID, projectID.String(), chatID.String()))
	require.NoError(t, ti.chConn.Exec(ctx, `
		INSERT INTO risk_findings (id, created_at, inserted_at, organization_id, project_id, rule_id, source, category, chat_id, false_positive_at)
		VALUES (?, ?, ?, ?, ?, 'secret.github_pat', 'gitleaks', 'secrets', ?, ?)
	`, dismissedID, from.Add(3*time.Hour), from.Add(4*time.Hour), orgID, projectID.String(), chatID.String(), from.Add(4*time.Hour)))

	result, err := ti.service.GetRiskOverview(ctx, &gen.GetRiskOverviewPayload{
		From: new(from.Format(time.RFC3339)),
		To:   new(to.Format(time.RFC3339)),
	})
	require.NoError(t, err)

	require.Equal(t, int64(1), result.Findings, "the dismissed id's stale original row must not be counted")

	categoryCounts := map[string]int64{}
	for _, category := range result.TopCategories {
		categoryCounts[category.Category] = category.Findings
	}
	require.Equal(t, int64(1), categoryCounts["secrets"])
}

// TestInsertRiskFindings_SameIDTwiceInOneBatchGetsDistinctInsertedAt guards
// against a regression where inserted_at was left off InsertRiskFindings'
// column list and fell back to the table's DEFAULT now64(9). ClickHouse
// evaluates now64() once per INSERT statement, not once per row, so every
// row in a single multi-row batch got the identical inserted_at. That's
// invisible when each id appears once per batch, but risk.markResultsFalsePositive
// can append a second row for an id that was JUST inserted (or a Pub/Sub
// backlog can redeliver several state changes for the same id into one
// batch), and an identical inserted_at makes the ROW_NUMBER() dedup in
// overviewFindings (overview.go) pick a non-deterministic row instead of the
// latest one.
func TestInsertRiskFindings_SameIDTwiceInOneBatchGetsDistinctInsertedAt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -1).Truncate(time.Hour)
	to := now.Add(time.Hour)

	chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, "bob@example.com")

	original := chOverviewFinding(t, projectID, orgID, chatID, msgID, from.Add(time.Hour), "gitleaks", "secret.stripe_api_key", "bob@example.com")
	dismissedAt := from.Add(2 * time.Hour)
	dismissed := original
	dismissed.FalsePositiveAt = &dismissedAt

	// Both rows share `original.ID` and land in ONE InsertRiskFindings call,
	// reproducing the same-batch double-insert scenario a rapid mark/unmark
	// (or a redelivered backlog) produces in production.
	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{original, dismissed}))

	rows, err := ti.chConn.Query(ctx, `SELECT toUnixTimestamp64Nano(inserted_at) FROM risk_findings WHERE id = ? ORDER BY inserted_at`, original.ID)
	require.NoError(t, err)
	var insertedAtsNanos []int64
	for rows.Next() {
		var nanos int64
		require.NoError(t, rows.Scan(&nanos))
		insertedAtsNanos = append(insertedAtsNanos, nanos)
	}
	require.NoError(t, rows.Err())
	require.Len(t, insertedAtsNanos, 2)
	require.Less(t, insertedAtsNanos[0], insertedAtsNanos[1],
		"the two rows for one id inserted in the same batch must get distinct, ordered inserted_at values, got %v", insertedAtsNanos)

	result, err := ti.service.GetRiskOverview(ctx, &gen.GetRiskOverviewPayload{
		From: new(from.Format(time.RFC3339)),
		To:   new(to.Format(time.RFC3339)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), result.Findings, "the dedup must resolve to the later (dismissed) row, not an arbitrary one")
}

// TestGetRiskOverview_ClickHouseUserEmailPrecedence covers the Go-side email
// resolution that replaces the Postgres users join: a resolvable internal user
// id wins over the external id, and the users lookup result merges groups.
func TestGetRiskOverview_ClickHouseUserEmailPrecedence(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	now := time.Now().UTC()
	from := now.AddDate(0, 0, -30).Truncate(24 * time.Hour) // 30 days ago, well within ClickHouse TTL
	to := from.AddDate(0, 0, 1)                             // 1 day window

	// The auth context user exists in the users table; findings attributed to
	// that internal user id must resolve to its email even with an opaque
	// external id.
	chatID, msgID := seedChatWithUser(t, ti, projectID, orgID, "opaque-external")
	internalUser := chOverviewFinding(t, projectID, orgID, chatID, msgID, from.Add(2*time.Hour), "gitleaks", "secret.github_pat", "opaque-external")
	internalUser.UserID = authCtx.UserID

	// Unknown internal user id + opaque external id: falls through to the
	// "Unknown user" bucket.
	unknownUser := chOverviewFinding(t, projectID, orgID, chatID, msgID, from.Add(3*time.Hour), "gitleaks", "secret.github_pat", "opaque-external")
	unknownUser.UserID = "user-that-does-not-exist"

	chQueries := chrepo.New(ti.chConn)
	require.NoError(t, chQueries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{internalUser, unknownUser}))

	require.NotNil(t, authCtx.Email)
	userEmail := *authCtx.Email

	result, err := ti.service.GetRiskOverview(ctx, &gen.GetRiskOverviewPayload{
		From: new(from.Format(time.RFC3339)),
		To:   new(to.Format(time.RFC3339)),
	})
	require.NoError(t, err)

	users := map[string]int64{}
	for _, user := range result.TopUsers {
		users[user.Email] = user.Findings
	}
	require.Equal(t, int64(1), users[userEmail])
	require.Equal(t, int64(1), users["Unknown user"])
}

func TestGetRiskUserBreakdownCountsOnlyLiveUserFindings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	active, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("User Breakdown Active")})
	require.NoError(t, err)
	disabled, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("User Breakdown Disabled"), Enabled: new(false)})
	require.NoError(t, err)
	deleted, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("User Breakdown Deleted")})
	require.NoError(t, err)

	at := time.Now().UTC().Add(-time.Hour)
	const targetUserID = "target-user"
	finding := func(policyID, source, ruleID, externalUserID string) chrepo.RiskFindingRow {
		row := chOverviewFinding(t, projectID, orgID, uuid.New(), uuid.New(), at, source, ruleID, externalUserID)
		row.RiskPolicyID = policyID
		return row
	}

	secret := finding(active.ID, "gitleaks", "secret.github_pat", targetUserID)
	// Agents report ids in whatever case they were configured with.
	financial := finding(active.ID, "presidio", "pii.credit_card", strings.ToUpper(targetUserID))
	dismissed := finding(active.ID, "gitleaks", "secret.dismissed", targetUserID)
	dismissed.FalsePositiveAt = &at
	excluded := finding(active.ID, "gitleaks", "secret.excluded", targetUserID)
	excluded.ExcludedAt = &at
	shadow := finding(active.ID, "gitleaks", "secret.shadow", targetUserID)
	shadow.Shadow = true

	require.NoError(t, chrepo.New(ti.chConn).InsertRiskFindings(ctx, []chrepo.RiskFindingRow{
		secret,
		secret,
		financial,
		dismissed,
		excluded,
		shadow,
		finding(active.ID, "gitleaks", "secret.other_user", "other-user"),
		finding(disabled.ID, "gitleaks", "secret.disabled", targetUserID),
		finding(deleted.ID, "gitleaks", "secret.deleted", targetUserID),
	}))

	deletedPolicyID, err := uuid.Parse(deleted.ID)
	require.NoError(t, err)
	require.NoError(t, riskrepo.New(ti.conn).DeleteRiskPolicy(ctx, riskrepo.DeleteRiskPolicyParams{
		ID:        deletedPolicyID,
		ProjectID: projectID,
	}))

	from := at.Add(-time.Hour).Format(time.RFC3339)
	to := at.Add(time.Hour).Format(time.RFC3339)
	result, err := ti.service.GetRiskUserBreakdown(ctx, &gen.GetRiskUserBreakdownPayload{
		ExternalUserID: targetUserID,
		From:           &from,
		To:             &to,
	})
	require.NoError(t, err)

	require.Equal(t, int64(2), result.Findings)
	require.Equal(t, []*gen.RiskOverviewCategory{
		{Category: "financial", Findings: 1},
		{Category: "secrets", Findings: 1},
	}, result.Categories)
	require.Equal(t, []*gen.RiskRuleBreakdownEntry{
		{RuleID: "pii.credit_card", Source: "presidio", Findings: 1},
		{RuleID: "secret.github_pat", Source: "gitleaks", Findings: 1},
	}, result.Rules)
}

func TestRiskFindingsTTLRemovesExpiredRows(t *testing.T) { //nolint:paralleltest // OPTIMIZE mutates the ClickHouse table shared by this package's tests.
	ctx, ti := newTestRiskService(t)

	const retention = 90 * 24 * time.Hour
	now := time.Now().UTC()
	projectID := uuid.New()
	orgID := "org_" + uuid.NewString()
	expired := chOverviewFinding(t, projectID, orgID, uuid.New(), uuid.New(), now.Add(-retention-24*time.Hour), "gitleaks", "secret.github_pat", "expired@example.com")
	retained := chOverviewFinding(t, projectID, orgID, uuid.New(), uuid.New(), now.Add(-retention+24*time.Hour), "gitleaks", "secret.github_pat", "retained@example.com")

	queries := chrepo.New(ti.chConn)
	require.NoError(t, queries.InsertRiskFindings(ctx, []chrepo.RiskFindingRow{expired, retained}))

	// TTL deletion normally happens during background merges. Force a merge so
	// this test observes the retention contract deterministically.
	require.NoError(t, ti.chConn.Exec(ctx, "OPTIMIZE TABLE risk_findings FINAL"))

	var expiredCount, retainedCount uint64
	require.NoError(t, ti.chConn.QueryRow(ctx, `
		SELECT countIf(id = ?), countIf(id = ?)
		FROM risk_findings
		WHERE organization_id = ? AND project_id = ?
	`, expired.ID, retained.ID, orgID, projectID.String()).Scan(&expiredCount, &retainedCount))
	require.Zero(t, expiredCount)
	require.Equal(t, uint64(1), retainedCount)
}
