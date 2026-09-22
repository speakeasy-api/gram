package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestListRiskFindingPoliciesIncludesDisabled(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})
	enabled, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Enabled"), Enabled: new(true)})
	require.NoError(t, err)
	disabled, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Disabled"), Enabled: new(false)})
	require.NoError(t, err)
	deleted, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{Name: new("Deleted")})
	require.NoError(t, err)
	require.NoError(t, ti.service.DeleteRiskPolicy(ctx, &gen.DeleteRiskPolicyPayload{ID: deleted.ID}))
	queries := riskrepo.New(ti.conn)
	params := riskrepo.ListRiskFindingPoliciesParams{ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID, PageLimit: 1001}
	rows, err := queries.ListRiskFindingPolicies(ctx, params)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID.String())
		require.False(t, row.Deleted)
		if row.ID.String() == disabled.ID {
			require.False(t, row.Enabled)
		}
	}
	require.ElementsMatch(t, []string{enabled.ID, disabled.ID}, ids)
	params.PageLimit = 1
	rows, err = queries.ListRiskFindingPolicies(ctx, params)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	params.OrganizationID = "<OTHER_ORG_ID>"
	rows, err = queries.ListRiskFindingPolicies(ctx, params)
	require.NoError(t, err)
	require.Empty(t, rows)
	params.OrganizationID = authCtx.ActiveOrganizationID
	params.ProjectID = uuid.New()
	rows, err = queries.ListRiskFindingPolicies(ctx, params)
	require.NoError(t, err)
	require.Empty(t, rows)
}
