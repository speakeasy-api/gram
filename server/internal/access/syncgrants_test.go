package access

import (
	"testing"

	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

func TestService_patchRoleGrants_preservesUnmentionedGrants(t *testing.T) {
	t.Parallel()

	ctx, _, conn := newInternalTestService(t)
	organizationID := "org_patch_grants_preserve_unmentioned"
	roleSlug := "custom-audiences"
	seedInternalOrganization(t, ctx, conn, organizationID)
	rolePrincipal := seedInternalRole(t, ctx, conn, organizationID, roleSlug)

	seedInternalGrant(t, ctx, conn, organizationID, rolePrincipal, string(authz.ScopeProjectRead), "project-old")
	seedInternalGrant(t, ctx, conn, organizationID, rolePrincipal, string(authz.ScopeRiskPolicyEvaluate), "policy-canonical")

	_, err := authz.PatchRoleGrantsTx(ctx, conn, organizationID, roleSlug, rolePrincipal.String(), []*authz.RoleGrant{
		{
			Scope:     string(authz.ScopeProjectWrite),
			Selectors: nil,
		},
	}, []*authz.RoleGrant{
		{
			Scope:     string(authz.ScopeProjectRead),
			Selectors: []authz.Selector{authz.NewSelector(authz.ScopeProjectRead, "project-old")},
		},
	})
	require.NoError(t, err)

	rows, err := accessrepo.New(conn).ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   rolePrincipal.String(),
	})
	require.NoError(t, err)
	require.Len(t, rows, 2)

	type grantResourceKey struct {
		scope      string
		resourceID string
	}

	got := make([]grantResourceKey, 0, len(rows))
	for _, row := range rows {
		selectors, err := authz.SelectorFromRow(row.Selectors)
		require.NoError(t, err)
		got = append(got, grantResourceKey{scope: row.Scope, resourceID: selectors.ResourceID()})
	}
	require.ElementsMatch(t, []grantResourceKey{
		{scope: string(authz.ScopeProjectWrite), resourceID: authz.WildcardResource},
		{scope: string(authz.ScopeRiskPolicyEvaluate), resourceID: "policy-canonical"},
	}, got)
}
