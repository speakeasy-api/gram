package telemetry_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
