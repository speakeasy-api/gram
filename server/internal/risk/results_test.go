package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// seedChatMessage creates a chat and message for the given project, returning the chat ID and message ID.
func seedChatMessage(t *testing.T, ti *testInstance, projectID uuid.UUID, orgID string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	chatID, err := uuid.NewV7()
	require.NoError(t, err)

	_, err = ti.chatRepo.UpsertChat(ctx, chatrepo.UpsertChatParams{
		ID:             chatID,
		ProjectID:      projectID,
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	msgID, err := testrepo.New(ti.conn).InsertChatMessage(ctx, testrepo.InsertChatMessageParams{
		ChatID:    chatID,
		ProjectID: uuid.NullUUID{UUID: projectID, Valid: true},
		Role:      "user",
		Content:   "test message with a secret",
	})
	require.NoError(t, err)

	return chatID, msgID
}

func seedRiskResult(t *testing.T, ti *testInstance, projectID uuid.UUID, orgID string, policyID uuid.UUID, policyVersion int64, msgID uuid.UUID, found bool) {
	t.Helper()
	ctx := t.Context()

	resultID, err := uuid.NewV7()
	require.NoError(t, err)

	repo := riskrepo.New(ti.conn)
	_, err = repo.InsertRiskResults(ctx, []riskrepo.InsertRiskResultsParams{{
		ID:                resultID,
		ProjectID:         projectID,
		OrganizationID:    orgID,
		RiskPolicyID:      policyID,
		RiskPolicyVersion: policyVersion,
		ChatMessageID:     msgID,
		Source:            "gitleaks",
		Found:             found,
		RuleID:            pgtype.Text{String: "aws-access-key-id", Valid: found},
		Description:       pgtype.Text{String: "AWS Access Key ID", Valid: found},
		Match:             pgtype.Text{String: "AKIAIOSFODNN7EXAMPLE", Valid: found},
		StartPos:          pgtype.Int4{Int32: 0, Valid: found},
		EndPos:            pgtype.Int4{Int32: 20, Valid: found},
		Confidence:        pgtype.Float8{Float64: 1.0, Valid: found},
		Tags:              nil,
	}})
	require.NoError(t, err)
}

func TestListRiskResults_ByPolicy(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Results Test")})
	require.NoError(t, err)

	policyID, _ := uuid.Parse(policy.ID)
	_, msgID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msgID, true)

	result, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		PolicyID: &policy.ID,
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	require.Equal(t, "aws-access-key-id", *result.Results[0].RuleID)
}

func TestListRiskResults_ByChatID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Chat Filter Test")})
	require.NoError(t, err)

	policyID, _ := uuid.Parse(policy.ID)
	chatID, msgID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msgID, true)

	chatIDStr := chatID.String()
	result, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{
		ChatID: &chatIDStr,
	})
	require.NoError(t, err)
	require.Len(t, result.Results, 1)
	require.Equal(t, chatIDStr, *result.Results[0].ChatID)
}

func TestListRiskResults_ExcludesNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Found Filter")})
	require.NoError(t, err)

	policyID, _ := uuid.Parse(policy.ID)
	_, msgID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msgID, false)

	result, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Results)
}

func TestGetRiskPolicyStatus_WithAnalyzedMessages(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Status Detail")})
	require.NoError(t, err)

	policyID, _ := uuid.Parse(policy.ID)

	// Seed 2 messages, analyze 1.
	_, msg1 := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msg1, true)

	status, err := ti.service.GetRiskPolicyStatus(ctx, &gen.GetRiskPolicyStatusPayload{ID: policy.ID})
	require.NoError(t, err)
	require.Equal(t, int64(2), status.TotalMessages)
	require.Equal(t, int64(1), status.AnalyzedMessages)
	require.Equal(t, int64(1), status.PendingMessages)
	require.Equal(t, int64(1), status.FindingsCount)
	require.Equal(t, "running", status.WorkflowStatus)
}

func TestGetRiskPolicyStatus_AllAnalyzed(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Complete")})
	require.NoError(t, err)

	policyID, _ := uuid.Parse(policy.ID)

	_, msg1 := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msg1, false)

	status, err := ti.service.GetRiskPolicyStatus(ctx, &gen.GetRiskPolicyStatusPayload{ID: policy.ID})
	require.NoError(t, err)
	require.Equal(t, int64(1), status.TotalMessages)
	require.Equal(t, int64(1), status.AnalyzedMessages)
	require.Equal(t, int64(0), status.PendingMessages)
	require.Equal(t, "sleeping", status.WorkflowStatus)
}

func TestTriggerRiskAnalysis_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	err := ti.service.TriggerRiskAnalysis(ctx, &gen.TriggerRiskAnalysisPayload{ID: uuid.New().String()})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestListRiskResults_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestGetRiskPolicyStatus_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.GetRiskPolicyStatus(ctx, &gen.GetRiskPolicyStatusPayload{ID: uuid.New().String()})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestListRiskPolicies_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.ListRiskPolicies(ctx, &gen.ListRiskPoliciesPayload{})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestGetRiskPolicy_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: uuid.New().String()})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestUpdateRiskPolicy_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{ID: uuid.New().String(), Name: "x"})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestDeleteRiskPolicy_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	err := ti.service.DeleteRiskPolicy(ctx, &gen.DeleteRiskPolicyPayload{ID: uuid.New().String()})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}
