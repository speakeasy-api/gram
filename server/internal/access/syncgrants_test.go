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
	seedInternalGrant(t, ctx, conn, organizationID, rolePrincipal, string(authz.ScopeProjectRead), "project-old")
	seedInternalGrant(t, ctx, conn, organizationID, rolePrincipal, string(authz.ScopeProjectWrite), "project-stale")
	seedInternalGrant(t, ctx, conn, organizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "other-role"), string(authz.ScopeProjectRead), "project-other")

	err := authz.SyncGrants(ctx, svc.logger, conn, organizationID, roleSlug, []*authz.RoleGrant{
		{
			Scope:     string(authz.ScopeProjectRead),
			Selectors: nil,
		},
		{
			Scope: string(authz.ScopeMCPConnect),
			Selectors: []authz.Selector{
				{"resource_kind": "mcp", "resource_id": "tool:payments"},
				{"resource_kind": "mcp", "resource_id": "tool:analytics"},
			},
		},
		{
			Scope:     string(authz.ScopeProjectWrite),
			Selectors: nil,
		},
	})
	require.NoError(t, err)

	rows, err := accessrepo.New(conn).ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   rolePrincipal.String(),
	})
	require.NoError(t, err)
	require.Len(t, rows, 4)

	got := make([]string, 0, len(rows))
	for _, row := range rows {
		selectors, err := authz.SelectorFromRow(row.Selectors, authz.Scope(row.Scope), row.Resource)
		require.NoError(t, err)
		got = append(got, row.Scope+"|"+selectors.ResourceID())
	}
	require.ElementsMatch(t, []string{
		string(authz.ScopeProjectRead) + "|" + authz.WildcardResource,
		string(authz.ScopeProjectWrite) + "|" + authz.WildcardResource,
		string(authz.ScopeMCPConnect) + "|tool:analytics",
		string(authz.ScopeMCPConnect) + "|tool:payments",
	}, got)

	otherRows, err := accessrepo.New(conn).ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   "role:other-role",
	})
	require.NoError(t, err)
	require.Len(t, otherRows, 1)
	otherSel, err := authz.SelectorFromRow(otherRows[0].Selectors, authz.Scope(otherRows[0].Scope), otherRows[0].Resource)
	require.NoError(t, err)
	require.Equal(t, "project-other", otherSel.ResourceID())
}

func TestService_syncGrants_emptySelectorsCreatesNoGrant(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newInternalTestService(t)
	organizationID := "org_sync_grants_empty_sel"
	roleSlug := "custom-empty-sel"
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug)

	seedInternalOrganization(t, ctx, conn, organizationID)

	// Empty non-nil selectors = no access (not wildcard).
	err := authz.SyncGrants(ctx, svc.logger, conn, organizationID, roleSlug, []*authz.RoleGrant{
		{
			Scope:     string(authz.ScopeMCPConnect),
			Selectors: []authz.Selector{},
		},
	})
	require.NoError(t, err)

	rows, err := accessrepo.New(conn).ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   rolePrincipal.String(),
	})
	require.NoError(t, err)
	require.Empty(t, rows, "empty selectors should produce zero grant rows, not a wildcard")
}

func TestService_syncGrants_clearsRoleGrantsWhenEmpty(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newInternalTestService(t)
	organizationID := "org_sync_grants_clear"
	roleSlug := "custom-viewer"
	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, roleSlug)

	seedInternalOrganization(t, ctx, conn, organizationID)
	seedInternalGrant(t, ctx, conn, organizationID, rolePrincipal, string(authz.ScopeProjectRead), authz.WildcardResource)
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
