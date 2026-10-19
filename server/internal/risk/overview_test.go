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
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestGetRiskOverview_DefaultWindow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	recentChat, recent := seedChatWithUser(t, ti, projectID, orgID, "recent@example.com")
	oldChat, old := seedChatWithUser(t, ti, projectID, orgID, "old@example.com")

	now := time.Now().UTC()
	require.NoError(t, chrepo.New(ti.chConn).InsertRiskFindings(ctx, []chrepo.RiskFindingRow{
		chOverviewFinding(t, projectID, orgID, recentChat, recent, now.Add(-time.Hour), "gitleaks", "secret.github_pat", "recent@example.com"),
		chOverviewFinding(t, projectID, orgID, oldChat, old, now.AddDate(0, 0, -10), "gitleaks", "secret.github_pat", "old@example.com"),
	}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	result, err := ti.service.GetRiskOverview(ctx, &gen.GetRiskOverviewPayload{})
	require.NoError(t, err)
	require.Equal(t, int64(1), result.Findings)
	require.NotEmpty(t, result.TimeSeriesFindings)
	require.Equal(t, "secrets", result.TimeSeriesFindings[0].Category)
}

func TestGetRiskOverview_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.GetRiskOverview(ctx, &gen.GetRiskOverviewPayload{})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestGetRiskOverview_RejectsLargeWindow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	from := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 32)
	_, err := ti.service.GetRiskOverview(ctx, &gen.GetRiskOverviewPayload{
		From: new(from.Format(time.RFC3339)),
		To:   new(to.Format(time.RFC3339)),
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

func TestGetRuleBreakdown_ExcludesDeletedPolicies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	live, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Breakdown Live")})
	require.NoError(t, err)
	deleted, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Breakdown Deleted")})
	require.NoError(t, err)
	deletedPolicyID, err := uuid.Parse(deleted.ID)
	require.NoError(t, err)

	now := time.Now().UTC()
	finding := func(policyID, ruleID string) chrepo.RiskFindingRow {
		chatID, msgID := seedChatMessage(t, ti, projectID, orgID)
		row := chOverviewFinding(t, projectID, orgID, chatID, msgID, now.Add(-time.Hour), "gitleaks", ruleID, "")
		row.RiskPolicyID = policyID
		return row
	}
	require.NoError(t, chrepo.New(ti.chConn).InsertRiskFindings(ctx, []chrepo.RiskFindingRow{
		finding(live.ID, "secret.stripe"),
		finding(deleted.ID, "secret.stripe"),
		finding(deleted.ID, "secret.aws"),
	}))
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	// Soft-delete through the repository so the findings stay behind, the way
	// they do until TTL in production.
	require.NoError(t, riskrepo.New(ti.conn).DeleteRiskPolicy(ctx, riskrepo.DeleteRiskPolicyParams{ID: deletedPolicyID, ProjectID: projectID}))

	result, err := ti.service.GetRuleBreakdown(ctx, "secrets", nil, nil)
	require.NoError(t, err)
	require.Len(t, result.Rules, 1)
	require.Equal(t, "secret.stripe", result.Rules[0].RuleID)
	require.EqualValues(t, 1, result.Rules[0].Findings)
	require.EqualValues(t, 1, result.Total)
}
