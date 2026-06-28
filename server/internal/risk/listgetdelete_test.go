package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestListRiskPolicies_Empty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	result, err := ti.service.ListRiskPolicies(ctx, &gen.ListRiskPoliciesPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Policies)
}

func TestListRiskPolicies_ReturnsPolicies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Policy A")})
	require.NoError(t, err)
	_, err = ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Policy B")})
	require.NoError(t, err)

	result, err := ti.service.ListRiskPolicies(ctx, &gen.ListRiskPoliciesPayload{})
	require.NoError(t, err)
	require.Len(t, result.Policies, 2)
}

func TestGetRiskPolicy_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Get Me")})
	require.NoError(t, err)

	got, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, created.ID, got.ID)
	require.Equal(t, "Get Me", got.Name)
}

func TestGetRiskPolicy_NotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: uuid.New().String()})
	require.Error(t, err)
}

func TestDeleteRiskPolicy_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Delete Me")})
	require.NoError(t, err)

	err = ti.service.DeleteRiskPolicy(ctx, &gen.DeleteRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)

	// Should no longer be found.
	_, err = ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: created.ID})
	require.Error(t, err)
}

func TestDeleteRiskPolicy_NotInList(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Delete Me")})
	require.NoError(t, err)

	err = ti.service.DeleteRiskPolicy(ctx, &gen.DeleteRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)

	result, err := ti.service.ListRiskPolicies(ctx, &gen.ListRiskPoliciesPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Policies)
}

func TestDeleteRiskPolicy_DeletesBypassRequests(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Delete Requests")})
	require.NoError(t, err)

	request, err := ti.service.CreateRiskPolicyBypassRequest(ctx, &gen.CreateRiskPolicyBypassRequestPayload{
		RequestToken: riskPolicyBypassRequestToken(t, ti, authCtx, created.ID, "https://mcp.example.com/delete-policy"),
	})
	require.NoError(t, err)
	require.Equal(t, created.ID, request.PolicyID)

	before, err := ti.service.ListRiskPolicyBypassRequests(ctx, &gen.ListRiskPolicyBypassRequestsPayload{
		PolicyID: &created.ID,
	})
	require.NoError(t, err)
	require.Len(t, before.Requests, 1)

	err = ti.service.DeleteRiskPolicy(ctx, &gen.DeleteRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)

	after, err := ti.service.ListRiskPolicyBypassRequests(ctx, &gen.ListRiskPolicyBypassRequestsPayload{
		PolicyID: &created.ID,
	})
	require.NoError(t, err)
	require.Empty(t, after.Requests)
}

func TestDeleteRiskPolicy_DeletesRiskResults(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Results Cleanup")})
	require.NoError(t, err)

	policyID, _ := uuid.Parse(policy.ID)
	_, msgID := seedChatMessage(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
	seedRiskResult(t, ti, *authCtx.ProjectID, authCtx.ActiveOrganizationID, policyID, 1, msgID, true)

	repo := riskrepo.New(ti.conn)
	before, err := repo.CountRiskResultsByPolicyID(t.Context(), riskrepo.CountRiskResultsByPolicyIDParams{
		RiskPolicyID: policyID,
		ProjectID:    *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), before)

	err = ti.service.DeleteRiskPolicy(ctx, &gen.DeleteRiskPolicyPayload{ID: policy.ID})
	require.NoError(t, err)

	after, err := repo.CountRiskResultsByPolicyID(t.Context(), riskrepo.CountRiskResultsByPolicyIDParams{
		RiskPolicyID: policyID,
		ProjectID:    *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), after)
}

func TestDeleteRiskPolicy_DeletesPolicyBoundExclusions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Exclusions Cleanup")})
	require.NoError(t, err)

	policyID, _ := uuid.Parse(policy.ID)
	repo := riskrepo.New(ti.conn)

	// Policy-bound exclusion.
	_, err = repo.CreateRiskExclusion(t.Context(), riskrepo.CreateRiskExclusionParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		RiskPolicyID:   uuid.NullUUID{UUID: policyID, Valid: true},
		MatchType:      "exact",
		MatchValue:     "secret",
		Enabled:        true,
	})
	require.NoError(t, err)

	// Global exclusion — must survive deletion.
	_, err = repo.CreateRiskExclusion(t.Context(), riskrepo.CreateRiskExclusionParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		RiskPolicyID:   uuid.NullUUID{},
		MatchType:      "exact",
		MatchValue:     "global-secret",
		Enabled:        true,
	})
	require.NoError(t, err)

	err = ti.service.DeleteRiskPolicy(ctx, &gen.DeleteRiskPolicyPayload{ID: policy.ID})
	require.NoError(t, err)

	// Policy-bound exclusion must be gone.
	bound, err := repo.ListRiskExclusionsByProject(t.Context(), riskrepo.ListRiskExclusionsByProjectParams{
		ProjectID:    *authCtx.ProjectID,
		RiskPolicyID: uuid.NullUUID{UUID: policyID, Valid: true},
	})
	require.NoError(t, err)
	require.Empty(t, bound)

	// Global exclusion must still exist.
	global, err := repo.ListRiskExclusionsByProject(t.Context(), riskrepo.ListRiskExclusionsByProjectParams{
		ProjectID:    *authCtx.ProjectID,
		RiskPolicyID: uuid.NullUUID{},
	})
	require.NoError(t, err)
	require.Len(t, global, 1)
}

func TestListRiskResults_EmptyProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	result, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Results)
}

func TestGetRiskPolicyStatus_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Status Test")})
	require.NoError(t, err)

	status, err := ti.service.GetRiskPolicyStatus(ctx, &gen.GetRiskPolicyStatusPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, created.ID, status.PolicyID)
	require.Equal(t, int64(1), status.PolicyVersion)
	require.Equal(t, int64(0), status.FindingsCount)
}

func TestTriggerRiskAnalysis_NotSupported(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	err := ti.service.TriggerRiskAnalysis(ctx, &gen.TriggerRiskAnalysisPayload{ID: uuid.New().String()})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotImplemented, oopsErr.Code)
}
