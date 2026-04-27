package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestListRiskPolicies_Empty(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))

	result, err := ti.service.ListRiskPolicies(ctx, &gen.ListRiskPoliciesPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Policies)
}

func TestListRiskPolicies_ReturnsPolicies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	)

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: "Policy A"})
	require.NoError(t, err)
	_, err = ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: "Policy B"})
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
		authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: "Get Me"})
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
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))

	_, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: uuid.New().String()})
	require.Error(t, err)
}

func TestDeleteRiskPolicy_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: "Delete Me"})
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
		authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: "Delete Me"})
	require.NoError(t, err)

	err = ti.service.DeleteRiskPolicy(ctx, &gen.DeleteRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)

	result, err := ti.service.ListRiskPolicies(ctx, &gen.ListRiskPoliciesPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Policies)
}

func TestListRiskResults_EmptyProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))

	result, err := ti.service.ListRiskResults(ctx, &gen.ListRiskResultsPayload{})
	require.NoError(t, err)
	require.Empty(t, result.Results)
}

func TestGetRiskPolicyStatus_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: "Status Test"})
	require.NoError(t, err)

	status, err := ti.service.GetRiskPolicyStatus(ctx, &gen.GetRiskPolicyStatusPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, created.ID, status.PolicyID)
	require.Equal(t, int64(1), status.PolicyVersion)
	require.Equal(t, int64(0), status.FindingsCount)
}

func TestTriggerRiskAnalysis_BumpsVersion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: "Trigger Test"})
	require.NoError(t, err)
	require.Equal(t, int64(1), created.Version)

	err = ti.service.TriggerRiskAnalysis(ctx, &gen.TriggerRiskAnalysisPayload{ID: created.ID})
	require.NoError(t, err)

	got, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, int64(2), got.Version, "trigger should bump version")
}
