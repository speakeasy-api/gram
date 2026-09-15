package telemetry_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// seedIdentityMapEntry writes one fold entry the way the sync worker does,
// minus the staging swap: ANY Join inserts are first-write-wins per key, and
// each test uses its own org id, so direct inserts cannot collide across
// parallel tests.
func seedIdentityMapEntry(t *testing.T, ctx context.Context, ti *testInstance, orgID, emailLower, userID, canonicalEmail string) {
	t.Helper()

	require.NoError(t, ti.chConn.Exec(ctx,
		"INSERT INTO identity_map (org_id, email_lower, canonical_user_id, canonical_email) VALUES (?, ?, ?, ?)",
		orgID, emailLower, userID, canonicalEmail))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
}

func foldTestContext(t *testing.T) (context.Context, *testInstance, string) {
	t.Helper()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeOrgRead,
		Selector: authz.NewSelector(authz.ScopeOrgRead, authCtx.ActiveOrganizationID),
	})
	return ctx, ti, authCtx.ActiveOrganizationID
}

func TestCostAnalytics_CanonicalFold_FiltersFoldThroughMap(t *testing.T) {
	t.Parallel()

	ctx, ti, orgID := foldTestContext(t)
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFold, orgID, true)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	suffix := uuid.NewString()[:8]
	workEmail := "work-" + suffix + "@example.com"
	personalEmail := "personal-" + suffix + "@example.com"
	userID := uuid.NewString()

	// The map is the only identity source on this path: no Postgres rows are
	// seeded, proving reads depend on identity_map alone.
	seedIdentityMapEntry(t, ctx, ti, orgID, workEmail, userID, workEmail)
	seedIdentityMapEntry(t, ctx, ti, orgID, personalEmail, userID, workEmail)

	now := time.Now().UTC()
	workChatID := uuid.NewString()
	personalChatID := uuid.NewString()
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, now.Add(-10*time.Minute), workChatID, 1, 100, 50, 0, 0, "opus", strings.ToUpper(workEmail[:1])+workEmail[1:], "Engineering", nil, "main", "", "", "", "")
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, now.Add(-9*time.Minute), personalChatID, 2, 200, 100, 0, 0, "opus", strings.ToUpper(personalEmail), "", nil, "main", "", "", "", "")
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, now.Add(-8*time.Minute), uuid.NewString(), 9, 900, 900, 0, 0, "opus", "stranger-"+suffix+"@example.com", "Engineering", nil, "main", "", "", "", "")
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)
	for _, email := range []string{workEmail, personalEmail, strings.ToUpper(personalEmail)} {
		result, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:    from,
			To:      to,
			Filters: []*gen.QueryFilter{{Dimension: "email", Values: []string{email}}},
			TopN:    10,
			SortBy:  "total_cost",
		})
		require.NoError(t, err)
		require.Len(t, result.Table, 1)
		require.InDelta(t, 3, result.Table[0].Measures.TotalCost, 0.001)
		require.Equal(t, int64(2), result.Table[0].Measures.TotalChats)

		sessions, err := ti.service.ListSessions(ctx, &gen.ListSessionsPayload{
			From:    from,
			To:      to,
			Filters: []*gen.QueryFilter{{Dimension: "email", Values: []string{email}}},
			SortBy:  "total_cost",
			Limit:   10,
		})
		require.NoError(t, err)
		require.Len(t, sessions.Sessions, 2)

		wideFrom, wideTo := summaryWindow(now)
		wideSessions := waitForListSessions(t, ctx, ti, &gen.ListSessionsPayload{
			From:    wideFrom,
			To:      wideTo,
			Filters: []*gen.QueryFilter{{Dimension: "email", Values: []string{email}}},
			SortBy:  "total_cost",
			Limit:   10,
		}, func(result *gen.ListSessionsResult) bool {
			return len(result.Sessions) == 2
		})
		require.Equal(t, personalChatID, wideSessions.Sessions[0].GramChatID)
		require.Equal(t, workChatID, wideSessions.Sessions[1].GramChatID)
	}
}

func TestCostAnalytics_CanonicalFold_GroupByEmailFoldsBuckets(t *testing.T) {
	t.Parallel()

	ctx, ti, orgID := foldTestContext(t)
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFold, orgID, true)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	suffix := uuid.NewString()[:8]
	workEmail := "gwork-" + suffix + "@example.com"
	personalEmail := "gpersonal-" + suffix + "@example.com"
	hostname := "ghost-" + suffix

	userID := uuid.NewString()
	seedIdentityMapEntry(t, ctx, ti, orgID, workEmail, userID, workEmail)
	seedIdentityMapEntry(t, ctx, ti, orgID, personalEmail, userID, workEmail)

	now := time.Now().UTC()
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, now.Add(-10*time.Minute), uuid.NewString(), 1, 100, 50, 0, 0, "opus", workEmail, "", nil, "main", "", "", "", "")
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, now.Add(-9*time.Minute), uuid.NewString(), 2, 200, 100, 0, 0, "opus", strings.ToUpper(personalEmail), "", nil, "main", "", "", "", "")
	insertAttributeClaudeAPIRequestLogWithHostname(t, ctx, projectID, now.Add(-8*time.Minute), uuid.NewString(), 4, "", hostname)
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.Query(ctx, &gen.QueryPayload{
		From:    now.Add(-time.Hour).Format(time.RFC3339),
		To:      now.Add(time.Hour).Format(time.RFC3339),
		GroupBy: conv.PtrEmpty("email"),
		TopN:    10,
		SortBy:  "total_cost",
	})
	require.NoError(t, err)

	costs := make(map[string]float64, len(result.Table))
	for _, row := range result.Table {
		costs[row.GroupValue] = row.Measures.TotalCost
	}
	// One employee = one bucket labeled with the canonical email; the device
	// hostname keeps its own literal bucket.
	require.InDelta(t, 3, costs[workEmail], 0.001)
	require.NotContains(t, costs, personalEmail)
	require.NotContains(t, costs, strings.ToUpper(personalEmail))
	require.InDelta(t, 4, costs[hostname], 0.001)

	// A hostname filter (no "@", so it skips the map entirely) still matches
	// case-insensitively — the literal path's semantics, preserved under fold.
	filtered, err := ti.service.Query(ctx, &gen.QueryPayload{
		From:    now.Add(-time.Hour).Format(time.RFC3339),
		To:      now.Add(time.Hour).Format(time.RFC3339),
		Filters: []*gen.QueryFilter{{Dimension: "email", Values: []string{strings.ToUpper(hostname)}}},
		TopN:    10,
		SortBy:  "total_cost",
	})
	require.NoError(t, err)
	require.Len(t, filtered.Table, 1)
	require.InDelta(t, 4, filtered.Table[0].Measures.TotalCost, 0.001)
}

func TestCostAnalytics_CanonicalFold_UnmappedEmailStaysLiteral(t *testing.T) {
	t.Parallel()

	ctx, ti, orgID := foldTestContext(t)
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFold, orgID, true)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	suffix := uuid.NewString()[:8]
	// Deliberately absent from identity_map — an ambiguous or unknown email.
	unmapped := "unmapped-" + suffix + "@example.com"

	now := time.Now().UTC()
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, now.Add(-10*time.Minute), uuid.NewString(), 5, 100, 50, 0, 0, "opus", strings.ToUpper(unmapped), "", nil, "main", "", "", "", "")
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// Case-insensitive literal matching survives for unmapped emails: both
	// sides fall back to lower() through the same expression.
	result, err := ti.service.Query(ctx, &gen.QueryPayload{
		From:    now.Add(-time.Hour).Format(time.RFC3339),
		To:      now.Add(time.Hour).Format(time.RFC3339),
		Filters: []*gen.QueryFilter{{Dimension: "email", Values: []string{unmapped}}},
		TopN:    10,
		SortBy:  "total_cost",
	})
	require.NoError(t, err)
	require.Len(t, result.Table, 1)
	require.InDelta(t, 5, result.Table[0].Measures.TotalCost, 0.001)
}

func TestCostAnalytics_CanonicalFold_SelfConsistentUnderMapLag(t *testing.T) {
	t.Parallel()

	ctx, ti, orgID := foldTestContext(t)
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFold, orgID, true)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	suffix := uuid.NewString()[:8]
	workEmail := "lagwork-" + suffix + "@example.com"
	personalEmail := "lagpersonal-" + suffix + "@example.com"

	now := time.Now().UTC()
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, now.Add(-10*time.Minute), uuid.NewString(), 1, 100, 50, 0, 0, "opus", workEmail, "", nil, "main", "", "", "", "")
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, now.Add(-9*time.Minute), uuid.NewString(), 2, 200, 100, 0, 0, "opus", personalEmail, "", nil, "main", "", "", "", "")
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	query := func(email string) float64 {
		result, err := ti.service.Query(ctx, &gen.QueryPayload{
			From:    now.Add(-time.Hour).Format(time.RFC3339),
			To:      now.Add(time.Hour).Format(time.RFC3339),
			Filters: []*gen.QueryFilter{{Dimension: "email", Values: []string{email}}},
			TopN:    10,
			SortBy:  "total_cost",
		})
		require.NoError(t, err)
		total := 0.0
		for _, row := range result.Table {
			total += row.Measures.TotalCost
		}
		return total
	}

	// Before the link syncs, both sides fold to the literal: the personal
	// drill sees exactly the personal usage — never a partial mix.
	require.InDelta(t, 2, query(personalEmail), 0.001)
	require.InDelta(t, 1, query(workEmail), 0.001)

	// The link arrives with the next map generation; both drills converge on
	// the folded totals.
	userID := uuid.NewString()
	seedIdentityMapEntry(t, ctx, ti, orgID, workEmail, userID, workEmail)
	seedIdentityMapEntry(t, ctx, ti, orgID, personalEmail, userID, workEmail)
	require.InDelta(t, 3, query(personalEmail), 0.001)
	require.InDelta(t, 3, query(workEmail), 0.001)
}

func TestCostAnalytics_CanonicalFold_ShadowServesLiteralResults(t *testing.T) {
	t.Parallel()

	ctx, ti, orgID := foldTestContext(t)
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFoldShadow, orgID, true)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	suffix := uuid.NewString()[:8]
	workEmail := "swork-" + suffix + "@example.com"
	personalEmail := "spersonal-" + suffix + "@example.com"
	userID := uuid.NewString()
	seedIdentityMapEntry(t, ctx, ti, orgID, workEmail, userID, workEmail)
	seedIdentityMapEntry(t, ctx, ti, orgID, personalEmail, userID, workEmail)

	now := time.Now().UTC()
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, now.Add(-10*time.Minute), uuid.NewString(), 1, 100, 50, 0, 0, "opus", workEmail, "", nil, "main", "", "", "", "")
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, now.Add(-9*time.Minute), uuid.NewString(), 2, 200, 100, 0, 0, "opus", personalEmail, "", nil, "main", "", "", "", "")
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// Shadow mode must not change served results: with no Postgres identity
	// links, the literal path matches only the requested email even though the
	// map would fold both.
	result, err := ti.service.Query(ctx, &gen.QueryPayload{
		From:    now.Add(-time.Hour).Format(time.RFC3339),
		To:      now.Add(time.Hour).Format(time.RFC3339),
		Filters: []*gen.QueryFilter{{Dimension: "email", Values: []string{workEmail}}},
		TopN:    10,
		SortBy:  "total_cost",
	})
	require.NoError(t, err)
	require.Len(t, result.Table, 1)
	require.InDelta(t, 1, result.Table[0].Measures.TotalCost, 0.001)
}

func TestEmployeeDetail_CanonicalFold_AllIdentifiersConverge(t *testing.T) {
	t.Parallel()

	// The employee detail endpoint runs under the default test grants (the
	// org-read-only context the aggregate tests use is not enough for it).
	ctx, ti := newTestLogsService(t)
	authCtx0, _ := contextvalues.GetAuthContext(ctx)
	orgID := authCtx0.ActiveOrganizationID
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFold, orgID, true)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.NewString()

	// The user-id identifier path keeps one directory lookup, so a connected
	// directory row is required; the map supplies everything else.
	employeeID, workEmail := seedConnectedOrgUser(t, ctx, ti, "fold-detail")
	workLower := strings.ToLower(workEmail)
	personalEmail := "fold-personal-" + uuid.NewString()[:8] + "@example.com"
	seedIdentityMapEntry(t, ctx, ti, orgID, workLower, employeeID, workLower)
	seedIdentityMapEntry(t, ctx, ti, orgID, personalEmail, employeeID, workLower)

	otherID, _ := seedConnectedOrgUser(t, ctx, ti, "fold-other")

	now := time.Now().UTC()
	// Email-only row under the personal account, user-id-only hook row, and a
	// DNO-509 trap: this employee's email on a row owned by someone else.
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), strings.ToUpper(personalEmail), 100, 50, 2.5)
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), employeeID, "", 700, 300, 42)
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), otherID, personalEmail, 9000, 9000, 999)
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	for _, identifier := range []string{employeeID, workLower, personalEmail} {
		m := userMetrics(t, ctx, ti, identifier)
		require.Equal(t, int64(800), m.TotalInputTokens, "identifier %s", identifier)
		require.InDelta(t, 44.5, m.TotalCost, 0.001, "identifier %s", identifier)
	}
}

func TestEmployeeDetail_CanonicalFold_UnfoldableOrgFallsBackToLegacyScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)

	// An org id that fails the SQL-literal allowlist can never drive the
	// in-query fold. The scope must degrade to the legacy expanded filter —
	// serving the detail page unfiltered would show every user's rows.
	badOrgID := "org'--" + uuid.NewString()[:8]
	ctx = switchOrganizationInCtx(t, ctx, badOrgID)
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFold, badOrgID, true)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.NewString()

	employeeID := uuid.NewString()
	strangerID := uuid.NewString()
	now := time.Now().UTC()
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), employeeID, "", 700, 300, 42)
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), strangerID, "", 9000, 9000, 999)
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	m := userMetrics(t, ctx, ti, employeeID)
	require.Equal(t, int64(700), m.TotalInputTokens)
}

func TestGetObservabilityOverview_CanonicalFold_SummaryScopesToUser(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	orgID := authCtx.ActiveOrganizationID
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFold, orgID, true)

	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.NewString()

	employeeID, _ := seedConnectedOrgUser(t, ctx, ti, "fold-overview")
	strangerID, _ := seedConnectedOrgUser(t, ctx, ti, "fold-overview-other")

	now := time.Now().UTC()
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), employeeID, "", 700, 300, 42)
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), strangerID, "", 9000, 9000, 999)
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// In fold mode the legacy User set stays empty, and with no other filters
	// the summary used to fall through to the unfiltered MV path and count
	// every user's rows while the rest of the overview stayed scoped.
	res, err := ti.service.GetObservabilityOverview(ctx, &gen.GetObservabilityOverviewPayload{
		From:   now.Add(-time.Hour).Format(time.RFC3339),
		To:     now.Add(time.Hour).Format(time.RFC3339),
		UserID: conv.PtrEmpty(employeeID),
	})
	require.NoError(t, err)
	require.Equal(t, int64(700), res.Summary.TotalInputTokens)
}

func TestEmployeeDetail_CanonicalFold_DeletedUserEmailNotFolded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	orgID := authCtx.ActiveOrganizationID
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFold, orgID, true)

	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.NewString()

	departedID, departedEmail := seedConnectedOrgUser(t, ctx, ti, "fold-departed")
	departedLower := strings.ToLower(departedEmail)
	require.NoError(t, testrepo.New(ti.conn).ForceSoftDeleteUser(ctx, departedID))

	// The identity map excludes deleted users, so the departed user's email
	// can already belong to an active owner in the map.
	activeID, activeEmail := seedConnectedOrgUser(t, ctx, ti, "fold-active")
	seedIdentityMapEntry(t, ctx, ti, orgID, departedLower, activeID, strings.ToLower(activeEmail))

	now := time.Now().UTC()
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), departedID, "", 700, 300, 42)
	// Email-only row that folds to the active owner: the departed user's page
	// must not sweep it in just because their directory row carries the email.
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), departedEmail, 100, 50, 2.5)
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	m := userMetrics(t, ctx, ti, departedID)
	require.Equal(t, int64(700), m.TotalInputTokens)
}

func TestSearchEmployeeAgentUsage_CanonicalFold_OneRowPerEmployee(t *testing.T) {
	t.Parallel()

	ctx, ti, orgID := foldTestContext(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	suffix := uuid.NewString()[:8]
	workEmail := "ework-" + suffix + "@example.com"
	personalEmail := "epersonal-" + suffix + "@example.com"
	userID := uuid.NewString()
	seedIdentityMapEntry(t, ctx, ti, orgID, workEmail, userID, workEmail)
	seedIdentityMapEntry(t, ctx, ti, orgID, personalEmail, userID, workEmail)

	now := time.Now().UTC()
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, now.Add(-10*time.Minute), uuid.NewString(), 1, 100, 50, 0, 0, "opus", workEmail, "", nil, "main", "", "", "", "")
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, now.Add(-9*time.Minute), uuid.NewString(), 2, 200, 100, 0, 0, "opus", strings.ToUpper(personalEmail), "", nil, "main", "", "", "", "")
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	params := repo.SearchEmployeeAgentUsageParams{
		GramProjectID:        projectID,
		TimeStart:            now.Add(-time.Hour).UnixNano(),
		TimeEnd:              now.Add(time.Hour).UnixNano(),
		Limit:                50,
		CanonicalIdentityOrg: orgID,
	}
	rows, err := ti.chClient.SearchEmployeeAgentUsage(ctx, params)
	require.NoError(t, err)

	byEmail := make(map[string]int64, len(rows))
	for _, row := range rows {
		byEmail[row.UserEmail] = row.TotalTokens
	}
	require.Equal(t, int64(450), byEmail[workEmail])
	require.NotContains(t, byEmail, personalEmail)
	require.NotContains(t, byEmail, strings.ToUpper(personalEmail))

	// Literal mode keeps the split rows — the flag-off behavior.
	params.CanonicalIdentityOrg = ""
	rows, err = ti.chClient.SearchEmployeeAgentUsage(ctx, params)
	require.NoError(t, err)
	byEmail = make(map[string]int64, len(rows))
	for _, row := range rows {
		byEmail[row.UserEmail] = row.TotalTokens
	}
	require.Equal(t, int64(150), byEmail[workEmail])
	require.Equal(t, int64(300), byEmail[strings.ToUpper(personalEmail)])
}

func TestGetTumBreakdownDimByDay_CanonicalFold_EmailSlicesFold(t *testing.T) {
	t.Parallel()

	ctx, ti, orgID := foldTestContext(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	projectID := authCtx.ProjectID.String()
	suffix := uuid.NewString()[:8]
	workEmail := "twork-" + suffix + "@example.com"
	personalEmail := "tpersonal-" + suffix + "@example.com"
	userID := uuid.NewString()
	seedIdentityMapEntry(t, ctx, ti, orgID, workEmail, userID, workEmail)
	seedIdentityMapEntry(t, ctx, ti, orgID, personalEmail, userID, workEmail)

	now := time.Now().UTC()
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, now.Add(-10*time.Minute), uuid.NewString(), 1, 100, 50, 0, 0, "opus", workEmail, "", nil, "main", "", "", "", "")
	insertAttributeClaudeAPIRequestLog(t, ctx, projectID, now.Add(-9*time.Minute), uuid.NewString(), 2, 200, 100, 0, 0, "opus", personalEmail, "", nil, "main", "", "", "", "")
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	params := repo.GetTokensUnderManagementParams{
		ProjectIDs:          []string{projectID},
		StartUnixNano:       now.Add(-time.Hour).UnixNano(),
		EndUnixNano:         now.Add(time.Hour).UnixNano(),
		ExcludedHookSources: billing.GramHostedHookSourceStrings(),
	}
	folded, err := ti.chClient.GetTumBreakdownDimByDay(ctx, params, "email", orgID)
	require.NoError(t, err)

	tokens := make(map[string]int64, len(folded))
	for _, bucket := range folded {
		tokens[bucket.Value] += bucket.Tokens
	}
	require.Equal(t, int64(450), tokens[workEmail])
	require.NotContains(t, tokens, personalEmail)

	literal, err := ti.chClient.GetTumBreakdownDimByDay(ctx, params, "email", "")
	require.NoError(t, err)
	tokens = make(map[string]int64, len(literal))
	for _, bucket := range literal {
		tokens[bucket.Value] += bucket.Tokens
	}
	require.Equal(t, int64(150), tokens[workEmail])
	require.Equal(t, int64(300), tokens[personalEmail])
}

func TestGetHooksSummary_CanonicalFold_UserDimensionFolds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	orgID := authCtx.ActiveOrganizationID
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFold, orgID, true)

	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.NewString()
	suffix := uuid.NewString()[:8]
	workEmail := "hwork-" + suffix + "@example.com"
	personalEmail := "hpersonal-" + suffix + "@example.com"
	strangerEmail := "hstranger-" + suffix + "@example.com"
	userID := uuid.NewString()
	seedIdentityMapEntry(t, ctx, ti, orgID, workEmail, userID, workEmail)
	seedIdentityMapEntry(t, ctx, ti, orgID, personalEmail, userID, workEmail)

	now := time.Now().UTC()
	event := func(email, tool, skill string) {
		insertHookEvent(t, ctx, hookEventParams{
			projectID:      projectID,
			deploymentID:   deploymentID,
			timestamp:      now.Add(-10 * time.Minute),
			traceID:        uuid.NewString(),
			userEmail:      email,
			hookSource:     "mcp",
			toolSource:     "server-a",
			toolName:       tool,
			result:         `"ok"`,
			skillName:      skill,
			conversationID: "conv-" + uuid.NewString()[:8],
		})
	}
	// Two personal-email events (one a Skill use), one work event with the
	// same skill, one stranger, one identity-less event (the Unknown bucket).
	event(strings.ToUpper(personalEmail), "weather", "")
	event(personalEmail, "Skill", "deploy-helper")
	event(workEmail, "Skill", "deploy-helper")
	event(strangerEmail, "weather", "")
	event("", "weather", "")
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	{
		res, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
			From: now.Add(-time.Hour).Format(time.RFC3339),
			To:   now.Add(time.Hour).Format(time.RFC3339),
		})
		require.NoError(t, err)
		require.NotNil(t, res)

		// One employee = one bucket under the canonical email; totals are
		// preserved (folding re-buckets, never drops or double-counts).
		byUser := map[string]int64{}
		var total int64
		for _, u := range res.Users {
			byUser[u.UserEmail] += u.EventCount
			total += u.EventCount
		}
		assert.Equal(t, int64(5), total)
		assert.Equal(t, int64(3), byUser[workEmail])
		assert.Equal(t, int64(1), byUser[strangerEmail])
		assert.Equal(t, int64(1), byUser["Unknown"])
		assert.NotContains(t, byUser, personalEmail)
		assert.NotContains(t, byUser, strings.ToUpper(personalEmail))

		// The skill breakdown folds the same way: the shared skill counts one
		// unique user, not two — the strongest signal the (skill, email)
		// grouping folded.
		for _, sk := range res.Skills {
			if sk.SkillName == "deploy-helper" {
				assert.Equal(t, int64(2), sk.UseCount)
				assert.Equal(t, int64(1), sk.UniqueUsers)
			}
		}

		// Breakdown and timeseries carry no unfolded personal buckets.
		for _, b := range res.Breakdown {
			assert.NotEqual(t, personalEmail, b.UserEmail)
			assert.NotEqual(t, strings.ToUpper(personalEmail), b.UserEmail)
		}
		for _, p := range res.TimeSeries {
			assert.NotEqual(t, personalEmail, p.UserEmail)
		}
	}

	// Drilling into the canonical bucket finds the personal rows too: both
	// sides of the filter fold through the same map.
	res, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
		From: now.Add(-time.Hour).Format(time.RFC3339),
		To:   now.Add(time.Hour).Format(time.RFC3339),
		Filters: []*gen.LogFilter{
			{Path: "user.email", Operator: "eq", Values: []string{workEmail}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), res.TotalEvents)
}

func TestHooksDrill_CanonicalFold_ExclusionAndTraceList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	orgID := authCtx.ActiveOrganizationID
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFold, orgID, true)

	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.NewString()
	suffix := uuid.NewString()[:8]
	workEmail := "xwork-" + suffix + "@example.com"
	personalEmail := "xpersonal-" + suffix + "@example.com"
	strangerEmail := "xstranger-" + suffix + "@example.com"
	userID := uuid.NewString()
	seedIdentityMapEntry(t, ctx, ti, orgID, workEmail, userID, workEmail)
	seedIdentityMapEntry(t, ctx, ti, orgID, personalEmail, userID, workEmail)

	now := time.Now().UTC()
	event := func(email string) {
		insertHookEvent(t, ctx, hookEventParams{
			projectID:      projectID,
			deploymentID:   deploymentID,
			timestamp:      now.Add(-10 * time.Minute),
			traceID:        uuid.NewString(),
			userEmail:      email,
			hookSource:     "mcp",
			toolSource:     "server-a",
			toolName:       "weather",
			result:         `"ok"`,
			skillName:      "",
			conversationID: "conv-" + uuid.NewString()[:8],
		})
	}
	event(workEmail)
	event(strings.ToUpper(personalEmail))
	event(strangerEmail)
	event("")
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)

	// not_eq excludes the whole folded identity — both linked emails — or the
	// excluded employee would reappear as a partial bucket.
	res, err := ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
		From: from,
		To:   to,
		Filters: []*gen.LogFilter{
			{Path: "user.email", Operator: "not_eq", Values: []string{workEmail}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), res.TotalEvents)

	// eq considers only the first value, matching the literal path's contract.
	res, err = ti.service.GetHooksSummary(ctx, &gen.GetHooksSummaryPayload{
		From: from,
		To:   to,
		Filters: []*gen.LogFilter{
			{Path: "user.email", Operator: "eq", Values: []string{workEmail, strangerEmail}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), res.TotalEvents)

	// The trace-list drill folds like the buckets that link to it: filtering
	// by the canonical email returns the personal-email trace too.
	traces, err := ti.service.ListHooksTraces(ctx, &gen.ListHooksTracesPayload{
		From:  from,
		To:    to,
		Limit: 50,
		Filters: []*gen.LogFilter{
			{Path: "user.email", Operator: "eq", Values: []string{workEmail}},
		},
	})
	require.NoError(t, err)
	require.Len(t, traces.Traces, 2)
}

func TestSearchUsers_CanonicalFold_CursorPagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	orgID := authCtx.ActiveOrganizationID
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFold, orgID, true)

	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.NewString()
	suffix := uuid.NewString()[:8]
	workEmail := "pwork-" + suffix + "@example.com"
	personalEmail := "ppersonal-" + suffix + "@example.com"
	strangerB := "pstrangerb-" + suffix + "@example.com"
	strangerC := "pstrangerc-" + suffix + "@example.com"
	userID := "gram-user-" + uuid.NewString()
	seedIdentityMapEntry(t, ctx, ti, orgID, workEmail, userID, workEmail)
	seedIdentityMapEntry(t, ctx, ti, orgID, personalEmail, userID, workEmail)

	// Staggered last-seen so page order is deterministic; the employee's most
	// recent row is the personal one, so the merged identity leads the list
	// and — at Limit 1 — becomes the page-1 cursor value, whose folded max
	// last-seen (-5m) differs from its literal one (-30m): cursor resolution
	// must go through the fold or page 2 comes back empty.
	now := time.Now().UTC()
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-30*time.Minute), userID, workEmail, 100, 50, 1.0)
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-5*time.Minute), strings.ToUpper(personalEmail), 200, 100, 2.0)
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), strangerB, 10, 10, 0.1)
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-20*time.Minute), strangerC, 20, 20, 0.2)
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)
	page := func(limit int, cursor *string) *gen.SearchUsersResult {
		res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
			Filter:   &gen.SearchUsersFilter{From: from, To: to},
			UserType: "internal",
			Limit:    limit,
			Sort:     "desc",
			Cursor:   cursor,
		})
		require.NoError(t, err)
		return res
	}

	// Limit 1 walks the list one group at a time, so page 1's cursor IS the
	// merged canonical key — the case a fold-unaware cursor subquery fails
	// (its literal max last-seen would mismatch the folded HAVING tuple and
	// page 2 would be empty).
	first := page(1, nil)
	require.Len(t, first.Users, 1)
	require.Equal(t, workEmail, first.Users[0].UserID, "the merged identity sorts by its latest (personal) row")
	require.Equal(t, int64(300), first.Users[0].TotalInputTokens, "the merged summary carries both emails' tokens")
	require.NotNil(t, first.NextCursor)
	require.Equal(t, workEmail, *first.NextCursor, "the cursor handed out is the merged canonical key")

	second := page(1, first.NextCursor)
	require.Len(t, second.Users, 1, "a canonical cursor resolves through the fold")
	require.Equal(t, strangerB, second.Users[0].UserID)
	require.NotNil(t, second.NextCursor)

	third := page(1, second.NextCursor)
	require.Len(t, third.Users, 1)
	require.Equal(t, strangerC, third.Users[0].UserID)

	// A wider page hands out an unmapped cursor instead; pagination must not
	// duplicate or resurrect the personal key either way.
	wideFirst := page(2, nil)
	require.Len(t, wideFirst.Users, 2)
	require.Equal(t, workEmail, wideFirst.Users[0].UserID)
	require.Equal(t, strangerB, wideFirst.Users[1].UserID)

	wideSecond := page(2, wideFirst.NextCursor)
	require.Len(t, wideSecond.Users, 1, "no duplicate and no resurrected personal key on page 2")
	require.Equal(t, strangerC, wideSecond.Users[0].UserID)
}

func TestSearchUsers_CanonicalFold_IDDrillReachesFoldedSummary(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	orgID := authCtx.ActiveOrganizationID
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFold, orgID, true)

	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.NewString()
	suffix := uuid.NewString()[:8]
	workEmail := "iwork-" + suffix + "@example.com"
	personalEmail := "ipersonal-" + suffix + "@example.com"
	userID := "gram-user-" + uuid.NewString()
	seedIdentityMapEntry(t, ctx, ti, orgID, workEmail, userID, workEmail)
	seedIdentityMapEntry(t, ctx, ti, orgID, personalEmail, userID, workEmail)

	now := time.Now().UTC()
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), userID, workEmail, 100, 50, 1.0)
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), personalEmail, 200, 100, 2.0)
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// An id-shaped key still reaches the summary that folded to an email:
	// rows match by raw user_id and group under the canonical key.
	res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From:    now.Add(-time.Hour).Format(time.RFC3339),
			To:      now.Add(time.Hour).Format(time.RFC3339),
			UserIds: []string{userID},
		},
		UserType: "internal",
		Limit:    100,
		Sort:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, res.Users, 1)
	require.Equal(t, workEmail, res.Users[0].UserID)
	require.Equal(t, int64(100), res.Users[0].TotalInputTokens, "the id drill matches the id-bearing rows")
}

func TestSearchUsers_CanonicalFold_RoleRollupCountsEmployeeOnce(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	orgID := authCtx.ActiveOrganizationID
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFold, orgID, true)

	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.NewString()
	suffix := uuid.NewString()[:8]
	workEmail := "rwork-" + suffix + "@example.com"
	personalEmail := "rpersonal-" + suffix + "@example.com"
	userID := "gram-user-" + uuid.NewString()
	seedIdentityMapEntry(t, ctx, ti, orgID, workEmail, userID, workEmail)
	seedIdentityMapEntry(t, ctx, ti, orgID, personalEmail, userID, workEmail)

	now := time.Now().UTC()
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), userID, workEmail, 100, 50, 1.0)
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), strings.ToUpper(personalEmail), 200, 100, 2.0)
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// No role assignments seeded, so everything lands in Unassigned — which is
	// exactly what pins the fold: one employee is one user in the rollup, not
	// one per linked email, and the totals merge losslessly.
	res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: now.Add(-time.Hour).Format(time.RFC3339),
			To:   now.Add(time.Hour).Format(time.RFC3339),
		},
		UserType: "internal",
		GroupBy:  "role",
		Limit:    100,
		Sort:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, res.Roles, 1)
	require.Equal(t, "Unassigned", res.Roles[0].RoleName)
	require.Equal(t, 1, res.Roles[0].UserCount, "one employee, not one per linked email")
	require.Equal(t, int64(300), res.Roles[0].TotalInputTokens)
}

func TestSearchUsers_CanonicalFold_OneRowPerEmployee(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	orgID := authCtx.ActiveOrganizationID
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFold, orgID, true)

	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.NewString()
	suffix := uuid.NewString()[:8]
	workEmail := "uwork-" + suffix + "@example.com"
	personalEmail := "upersonal-" + suffix + "@example.com"
	strangerEmail := "ustranger-" + suffix + "@example.com"
	userID := "gram-user-" + uuid.NewString()
	seedIdentityMapEntry(t, ctx, ti, orgID, workEmail, userID, workEmail)
	seedIdentityMapEntry(t, ctx, ti, orgID, personalEmail, userID, workEmail)

	now := time.Now().UTC()
	// One employee across three attribution shapes: id+work email, a
	// case-variant personal-email row, and an id-only tool call (folds in via
	// the known-emails join). Plus an unmapped stranger.
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), userID, workEmail, 100, 50, 1.0)
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), strings.ToUpper(personalEmail), 200, 100, 2.0)
	insertToolCallLogWithUser(t, ctx, projectID, deploymentID, now.Add(-8*time.Minute), "tools:http:petstore:listPets", 200, 0.5, userID, "")
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-7*time.Minute), strangerEmail, 9, 9, 0.1)
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)

	res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter:   &gen.SearchUsersFilter{From: from, To: to},
		UserType: "internal",
		Limit:    100,
		Sort:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, res.Users, 2)

	byKey := map[string]*gen.UserSummary{}
	for _, u := range res.Users {
		byKey[u.UserID] = u
	}
	employee := byKey[workEmail]
	require.NotNil(t, employee, "employee summary keyed by canonical email")
	// Sum preservation across all three attribution shapes; the display email
	// matches the canonical key rather than a literal variant.
	require.Equal(t, workEmail, employee.UserEmail)
	require.Equal(t, int64(300), employee.TotalInputTokens)
	require.Equal(t, int64(150), employee.TotalOutputTokens)
	require.Equal(t, int64(1), employee.TotalToolCalls)
	require.Contains(t, byKey, strangerEmail)
	require.NotContains(t, byKey, personalEmail)
	require.NotContains(t, byKey, strings.ToUpper(personalEmail))

	// Drilling by a linked personal email finds the one canonical summary:
	// both sides of the key filter fold through the same map.
	filtered, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter:   &gen.SearchUsersFilter{From: from, To: to, UserIds: []string{personalEmail}},
		UserType: "internal",
		Limit:    100,
		Sort:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, filtered.Users, 1)
	require.Equal(t, workEmail, filtered.Users[0].UserID)
	require.Equal(t, int64(300), filtered.Users[0].TotalInputTokens)
}

func TestSearchUsers_CanonicalFold_ShadowServesLiteralList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	orgID := authCtx.ActiveOrganizationID
	ti.featureFlags.SetFlag(feature.FlagCanonicalIdentityFoldShadow, orgID, true)

	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.NewString()
	suffix := uuid.NewString()[:8]
	workEmail := "vwork-" + suffix + "@example.com"
	personalEmail := "vpersonal-" + suffix + "@example.com"
	userID := "gram-user-" + uuid.NewString()
	seedIdentityMapEntry(t, ctx, ti, orgID, workEmail, userID, workEmail)
	seedIdentityMapEntry(t, ctx, ti, orgID, personalEmail, userID, workEmail)

	now := time.Now().UTC()
	insertPollingLogWithUserAndEmail(t, ctx, projectID, deploymentID, now.Add(-10*time.Minute), userID, workEmail, 100, 50, 1.0)
	insertPollingLogWithEmail(t, ctx, projectID, deploymentID, now.Add(-9*time.Minute), strings.ToUpper(personalEmail), 200, 100, 2.0)
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// Shadow mode serves the literal list — the employee still splits across
	// their two email keys — while the folded comparison runs detached.
	res, err := ti.service.SearchUsers(ctx, &gen.SearchUsersPayload{
		Filter: &gen.SearchUsersFilter{
			From: now.Add(-time.Hour).Format(time.RFC3339),
			To:   now.Add(time.Hour).Format(time.RFC3339),
		},
		UserType: "internal",
		Limit:    100,
		Sort:     "desc",
	})
	require.NoError(t, err)
	require.Len(t, res.Users, 2)
	keys := []string{res.Users[0].UserID, res.Users[1].UserID}
	require.Contains(t, keys, workEmail)
	require.Contains(t, keys, strings.ToUpper(personalEmail))
}

func TestGetUnproxiedMcpServerUserUsage_CanonicalFold_OneRowPerEmployee(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestLogsService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	orgID := authCtx.ActiveOrganizationID

	projectID := authCtx.ProjectID.String()
	deploymentID := uuid.NewString()
	suffix := uuid.NewString()[:8]
	workEmail := "uwork-" + suffix + "@example.com"
	personalEmail := "upersonal-" + suffix + "@example.com"
	userID := uuid.NewString()
	seedIdentityMapEntry(t, ctx, ti, orgID, workEmail, userID, workEmail)
	seedIdentityMapEntry(t, ctx, ti, orgID, personalEmail, userID, workEmail)

	serverURL := "https://mcp-" + suffix + ".example.com/api"
	now := time.Now().UTC()
	for _, email := range []string{workEmail, personalEmail} {
		insertHookEvent(t, ctx, hookEventParams{
			projectID:      projectID,
			deploymentID:   deploymentID,
			timestamp:      now.Add(-10 * time.Minute),
			traceID:        uuid.NewString(),
			userEmail:      email,
			hookSource:     "mcp",
			toolSource:     "server-u",
			toolName:       "tool",
			result:         `"ok"`,
			mcpServerURL:   serverURL,
			conversationID: "conv-" + uuid.NewString()[:8],
		})
	}

	params := repo.GetUnproxiedMcpServerUserUsageParams{
		GramProjectID:        projectID,
		CanonicalURL:         serverURL,
		TimeStart:            now.Add(-time.Hour).UnixNano(),
		TimeEnd:              now.Add(time.Hour).UnixNano(),
		Cursor:               "",
		Limit:                50,
		CanonicalIdentityOrg: orgID,
	}
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)
	rows, _, err := ti.chClient.GetUnproxiedMcpServerUserUsage(ctx, params)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, workEmail, rows[0].UserEmail)
	require.Equal(t, uint64(2), rows[0].CallCount)

	// Literal mode keeps the split rows — the flag-off behavior.
	params.CanonicalIdentityOrg = ""
	rows, _, err = ti.chClient.GetUnproxiedMcpServerUserUsage(ctx, params)
	require.NoError(t, err)
	require.Len(t, rows, 2)
}
