package admin

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestSetupRBAC_SeedsSystemRolesAndGrants(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	const organizationID = "org_legacy_rbac"
	seedOrg(t, ctx, conn, orgFixture{id: organizationID, name: "Legacy Org", slug: "legacy-org", whitelisted: true})

	err := svc.SetupRBAC(ctx, &gen.SetupRBACPayload{ID: organizationID})
	require.NoError(t, err)

	for _, roleSlug := range []string{authz.SystemRoleAdmin, authz.SystemRoleMember} {
		role, err := accessrepo.New(conn).GetGlobalRoleBySlug(ctx, roleSlug)
		require.NoError(t, err)

		grants, err := authz.GrantsForRole(ctx, svc.logger, conn, organizationID, "role:global:"+role.ID.String())
		require.NoError(t, err)
		require.NotEmpty(t, grants)
	}

	require.NoError(t, svc.SetupRBAC(ctx, &gen.SetupRBACPayload{ID: organizationID}))
}

func TestSetupRBAC_UnknownOrganization(t *testing.T) {
	t.Parallel()

	ctx, svc, _ := newTestAdminService(t)
	err := svc.SetupRBAC(ctx, &gen.SetupRBACPayload{ID: "org_missing"})

	requireOopsCode(t, err, oops.CodeNotFound)
}
