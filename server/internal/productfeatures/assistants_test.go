package productfeatures_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestEnableRBACTxPatchesAssistantDefaultsForExistingRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	seedOrganization(t, ctx, ti.conn, organizationID)
	require.NoError(t, productfeatures.EnableRBACTx(ctx, ti.conn, organizationID))

	q := accessrepo.New(ti.conn)
	admin := systemRolePrincipal(t, ctx, q, authz.SystemRoleAdmin)
	member := systemRolePrincipal(t, ctx, q, authz.SystemRoleMember)
	deleteGrant(t, ctx, q, organizationID, admin, authz.ScopeAssistantRead, authz.WildcardResource)
	deleteGrant(t, ctx, q, organizationID, admin, authz.ScopeAssistantWrite, authz.WildcardResource)
	deleteGrant(t, ctx, q, organizationID, member, authz.ScopeAssistantRead, authz.WildcardResource)
	upsertGrant(t, ctx, q, organizationID, member, authz.ScopeProjectRead, "project-retained")

	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, productfeatures.EnableRBACTx(ctx, tx, organizationID))
	require.NoError(t, tx.Commit(ctx))

	grants := organizationGrantKeys(t, ctx, q, organizationID)
	require.Equal(t, 1, grants[grantKey(admin, authz.ScopeAssistantRead, authz.WildcardResource)])
	require.Equal(t, 1, grants[grantKey(admin, authz.ScopeAssistantWrite, authz.WildcardResource)])
	require.Equal(t, 1, grants[grantKey(member, authz.ScopeAssistantRead, authz.WildcardResource)])
	require.Equal(t, 1, grants[grantKey(member, authz.ScopeProjectRead, "project-retained")])
}
