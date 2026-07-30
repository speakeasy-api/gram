package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestMarkUnmarkRiskResultsFalsePositive(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("False Positive Test")})
	require.NoError(t, err)
	policyID, err2 := uuid.Parse(policy.ID)
	require.NoError(t, err2)

	_, msgID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msgID, true)

	before, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{PolicyID: &policy.ID})
	require.NoError(t, err)
	require.Len(t, before.Results, 1)
	resultID := before.Results[0].ID

	dismissCountBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskResultDismiss)
	require.NoError(t, err)

	// Mark as false positive: it must disappear from listRiskResults and show
	// up in listDismissedResults, and it must be atomic with one audit entry.
	err = ti.service.MarkRiskResultsFalsePositive(ctx, &gen.MarkRiskResultsFalsePositivePayload{
		ResultIds: []string{resultID},
		Reason:    new("noise"),
	})
	require.NoError(t, err)

	dismissCountAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskResultDismiss)
	require.NoError(t, err)
	require.Equal(t, dismissCountBefore+1, dismissCountAfter)

	afterMark, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{PolicyID: &policy.ID})
	require.NoError(t, err)
	require.Empty(t, afterMark.Results, "dismissed result must not appear in listRiskResults")

	dismissed, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{})
	require.NoError(t, err)
	require.Len(t, dismissed.Results, 1)
	require.Equal(t, resultID, dismissed.Results[0].ID)
	require.NotNil(t, dismissed.Results[0].FalsePositiveAt)

	restoreCountBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskResultRestore)
	require.NoError(t, err)

	// Undo: the result must come back in listRiskResults and disappear from
	// listDismissedResults.
	err = ti.service.UnmarkRiskResultsFalsePositive(ctx, &gen.UnmarkRiskResultsFalsePositivePayload{
		ResultIds: []string{resultID},
	})
	require.NoError(t, err)

	restoreCountAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRiskResultRestore)
	require.NoError(t, err)
	require.Equal(t, restoreCountBefore+1, restoreCountAfter)

	afterUnmark, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{PolicyID: &policy.ID})
	require.NoError(t, err)
	require.Len(t, afterUnmark.Results, 1)

	dismissedAfterUnmark, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{})
	require.NoError(t, err)
	require.Empty(t, dismissedAfterUnmark.Results)
}

func TestMarkRiskResultsFalsePositive_RejectsEmptyIDs(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	err := ti.service.MarkRiskResultsFalsePositive(ctx, &gen.MarkRiskResultsFalsePositivePayload{ResultIds: nil})
	require.Error(t, err)
}
