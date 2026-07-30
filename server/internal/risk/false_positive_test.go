package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
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

	// The reason isn't surfaced on the API type today (no UI sends one yet),
	// but it must still land in Postgres from the RPC payload.
	resultUUID, err := uuid.Parse(resultID)
	require.NoError(t, err)
	dismissedRows, err := riskrepo.New(ti.conn).ListFalsePositiveRiskResults(ctx, riskrepo.ListFalsePositiveRiskResultsParams{
		ProjectID: *authCtx.ProjectID,
		PageLimit: 10,
	})
	require.NoError(t, err)
	require.Len(t, dismissedRows, 1)
	require.Equal(t, resultUUID, dismissedRows[0].ID)
	require.True(t, dismissedRows[0].FalsePositiveReason.Valid)
	require.Equal(t, "noise", dismissedRows[0].FalsePositiveReason.String)

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

func TestMarkRiskResultsFalsePositive_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn) // no org:admin grant

	err := ti.service.MarkRiskResultsFalsePositive(ctx, &gen.MarkRiskResultsFalsePositivePayload{
		ResultIds: []string{uuid.NewString()},
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestUnmarkRiskResultsFalsePositive_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn) // no org:admin grant

	err := ti.service.UnmarkRiskResultsFalsePositive(ctx, &gen.UnmarkRiskResultsFalsePositivePayload{
		ResultIds: []string{uuid.NewString()},
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestListDismissedRiskResults_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn) // no org:admin grant

	_, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

// TestMarkRiskResultsFalsePositive_RejectsBatchTooLarge exercises the
// maxFalsePositiveBatch guard (matching the "no batch job" scoping decision
// in the plan: multiselect is bounded, so a page-sized cap is enough).
func TestMarkRiskResultsFalsePositive_RejectsBatchTooLarge(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	ids := make([]string, 501)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	err := ti.service.MarkRiskResultsFalsePositive(ctx, &gen.MarkRiskResultsFalsePositivePayload{ResultIds: ids})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeInvalid, oopsErr.Code)
}

// TestMarkRiskResultsFalsePositive_ContentPartAnchoredFindingIsListable
// guards against a real regression: ListFalsePositiveRiskResults originally
// INNER JOINed chat_messages, which silently dropped any dismissed finding
// anchored to a chat_content_part_id instead (per risk_results_anchor_check,
// a result carries exactly one of the two) — such a finding could be marked
// false positive but would never appear in the Dismissed tab, making it
// permanently unrestorable through the UI.
func TestMarkRiskResultsFalsePositive_ContentPartAnchoredFindingIsListable(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Content Part False Positive Test")})
	require.NoError(t, err)
	policyID, err := uuid.Parse(policy.ID)
	require.NoError(t, err)

	chatID, msgID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)

	repo := riskrepo.New(ti.conn)
	partID, err := repo.CreateChatContentPartForTest(ctx, riskrepo.CreateChatContentPartForTestParams{
		ChatID:              chatID,
		ProjectID:           uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true},
		Kind:                "prompt_attachment",
		ContentAssetUrl:     "gs://test-bucket/content-part.txt",
		ParentChatMessageID: uuid.NullUUID{UUID: msgID, Valid: true},
	})
	require.NoError(t, err)

	resultID, err := uuid.NewV7()
	require.NoError(t, err)
	match := "SECRET_ATTACHMENT_TOKEN"
	_, err = repo.InsertRiskResults(ctx, []riskrepo.InsertRiskResultsParams{{
		ID:                resultID,
		ProjectID:         *authCtx.ProjectID,
		OrganizationID:    authCtx.ActiveOrganizationID,
		RiskPolicyID:      policyID,
		RiskPolicyVersion: 1,
		ChatMessageID:     uuid.NullUUID{},
		ChatContentPartID: uuid.NullUUID{UUID: partID, Valid: true},
		Source:            "gitleaks",
		Found:             true,
		RuleID:            pgtype.Text{String: "generic-api-key", Valid: true},
		Description:       pgtype.Text{String: "Generic API key", Valid: true},
		Match:             pgtype.Text{String: match, Valid: true},
		StartPos:          pgtype.Int4{Int32: 0, Valid: true},
		EndPos:            pgtype.Int4{Int32: int32(len(match)), Valid: true},
		Confidence:        pgtype.Float8{Float64: 1.0, Valid: true},
	}})
	require.NoError(t, err)

	err = ti.service.MarkRiskResultsFalsePositive(ctx, &gen.MarkRiskResultsFalsePositivePayload{
		ResultIds: []string{resultID.String()},
	})
	require.NoError(t, err)

	dismissed, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{})
	require.NoError(t, err)
	require.Len(t, dismissed.Results, 1, "a content-part-anchored dismissal must still be listable")
	require.Equal(t, resultID.String(), dismissed.Results[0].ID)
	require.NotNil(t, dismissed.Results[0].FalsePositiveAt)

	err = ti.service.UnmarkRiskResultsFalsePositive(ctx, &gen.UnmarkRiskResultsFalsePositivePayload{
		ResultIds: []string{resultID.String()},
	})
	require.NoError(t, err)

	afterUndo, err := ti.service.ListDismissedRiskResults(ctx, &gen.ListDismissedRiskResultsPayload{})
	require.NoError(t, err)
	require.Empty(t, afterUndo.Results)
}
