package access

import (
	"testing"

	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestService_syncGrants_replacesRoleGrants(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newInternalTestService(t)
	organizationID := "org_sync_grants_replace"
	roleSlug := "custom-editor"
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug)

	seedInternalOrganization(t, ctx, conn, organizationID)
	seedInternalGrant(t, ctx, conn, organizationID, rolePrincipal, string(authz.ScopeBuildRead), "project-old")
	seedInternalGrant(t, ctx, conn, organizationID, rolePrincipal, string(authz.ScopeBuildWrite), "project-stale")
	seedInternalGrant(t, ctx, conn, organizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "other-role"), string(authz.ScopeBuildRead), "project-other")

	err := authz.SyncGrants(ctx, svc.logger, conn, organizationID, roleSlug, []*authz.RoleGrant{
		{
			Scope:     string(authz.ScopeBuildRead),
			Resources: nil,
		},
		{
			Scope:     string(authz.ScopeMCPConnect),
			Resources: []string{"tool:payments", "tool:analytics"},
		},
		{
			Scope:     string(authz.ScopeBuildWrite),
			Resources: []string{},
		},
	})
	require.NoError(t, err)

	rows, err := accessrepo.New(conn).ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   rolePrincipal.String(),
	})
	require.NoError(t, err)
	require.Len(t, rows, 3)

	got := make([]string, 0, len(rows))
	for _, row := range rows {
		got = append(got, row.Scope+"|"+row.Resource)
	}
	require.ElementsMatch(t, []string{
		string(authz.ScopeBuildRead) + "|" + authz.WildcardResource,
		string(authz.ScopeMCPConnect) + "|tool:analytics",
		string(authz.ScopeMCPConnect) + "|tool:payments",
	}, got)

	otherRows, err := accessrepo.New(conn).ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   "role:other-role",
	})
	require.NoError(t, err)
	require.Len(t, otherRows, 1)
	require.Equal(t, "project-other", otherRows[0].Resource)
}

func TestService_syncGrants_clearsRoleGrantsWhenEmpty(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newInternalTestService(t)
	organizationID := "org_sync_grants_clear"
	roleSlug := "custom-viewer"
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug)

	seedInternalOrganization(t, ctx, conn, organizationID)
	seedInternalGrant(t, ctx, conn, organizationID, rolePrincipal, string(authz.ScopeBuildRead), authz.WildcardResource)
	seedInternalGrant(t, ctx, conn, organizationID, rolePrincipal, string(authz.ScopeMCPRead), "tool:payments")

	err := authz.SyncGrants(ctx, svc.logger, conn, organizationID, roleSlug, nil)
	require.NoError(t, err)

	rows, err := accessrepo.New(conn).ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   rolePrincipal.String(),
	})
	require.NoError(t, err)
	require.Empty(t, rows)
}
