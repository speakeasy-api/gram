package productfeatures_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
	adminShadow := createShadowingRole(t, ctx, q, organizationID, authz.SystemRoleAdmin)
	memberShadow := createShadowingRole(t, ctx, q, organizationID, authz.SystemRoleMember)

	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, productfeatures.EnableRBACTx(ctx, tx, organizationID))
	require.NoError(t, tx.Commit(ctx))

	grants := organizationGrantKeys(t, ctx, q, organizationID)
	require.Equal(t, 1, grants[grantKey(admin, authz.ScopeAssistantRead, authz.WildcardResource)])
	require.Equal(t, 1, grants[grantKey(admin, authz.ScopeAssistantWrite, authz.WildcardResource)])
	require.Equal(t, 1, grants[grantKey(member, authz.ScopeAssistantRead, authz.WildcardResource)])
	require.Equal(t, 1, grants[grantKey(member, authz.ScopeProjectRead, "project-retained")])
	require.Zero(t, grants[grantKey(adminShadow, authz.ScopeAssistantRead, authz.WildcardResource)])
	require.Zero(t, grants[grantKey(adminShadow, authz.ScopeAssistantWrite, authz.WildcardResource)])
	require.Zero(t, grants[grantKey(memberShadow, authz.ScopeAssistantRead, authz.WildcardResource)])
	require.Zero(t, grants[grantKey(memberShadow, authz.ScopeAssistantWrite, authz.WildcardResource)])
}

func createShadowingRole(t *testing.T, ctx context.Context, q *accessrepo.Queries, organizationID, slug string) urn.Principal {
	t.Helper()
	now := pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	role, err := q.CreateOrganizationRole(ctx, accessrepo.CreateOrganizationRoleParams{
		OrganizationID:    organizationID,
		WorkosSlug:        slug,
		WorkosName:        "Shadow role",
		WorkosDescription: pgtype.Text{},
		WorkosCreatedAt:   now,
		WorkosUpdatedAt:   now,
		WorkosLastEventID: pgtype.Text{},
	})
	require.NoError(t, err)
	principal, err := urn.ParsePrincipal(role.RoleUrn)
	require.NoError(t, err)
	return principal
}
