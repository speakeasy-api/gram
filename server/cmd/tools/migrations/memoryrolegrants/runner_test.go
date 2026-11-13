package memoryrolegrants_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/memoryrolegrants"
	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	readCheck  = authz.Check{Scope: authz.ScopeMemoryRead, ResourceKind: "", ResourceID: "project_1", Dimensions: nil}
	writeCheck = authz.Check{Scope: authz.ScopeMemoryWrite, ResourceKind: "", ResourceID: "project_1", Dimensions: nil}
)

func TestRunBackfillsLegacySystemRolesOnce(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	orgID := seedOrganization(t, ctx, pool)
	admin := seedLegacySystemRole(t, ctx, pool, orgID, authz.SystemRoleAdmin)
	member := globalRolePrincipal(t, ctx, pool, authz.SystemRoleMember)
	custom := urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+uuid.NewString())
	seedGrant(t, ctx, pool, orgID, custom, authz.ScopeProjectRead)

	report, err := memoryrolegrants.Run(ctx, pool, memoryrolegrants.Options{OrganizationID: "", Apply: false})
	require.NoError(t, err)
	require.Equal(t, memoryrolegrants.Report{Roles: 1, GrantsAdded: 2}, report)
	requireAuthorized(t, ctx, pool, orgID, admin, readCheck, false)

	report, err = memoryrolegrants.Run(ctx, pool, memoryrolegrants.Options{OrganizationID: "", Apply: true})
	require.NoError(t, err)
	require.Equal(t, memoryrolegrants.Report{Roles: 1, GrantsAdded: 2}, report)
	requireAuthorized(t, ctx, pool, orgID, member, readCheck, false)
	requireAuthorized(t, ctx, pool, orgID, admin, readCheck, true)
	requireAuthorized(t, ctx, pool, orgID, admin, writeCheck, true)
	requireAuthorized(t, ctx, pool, orgID, custom, readCheck, false)

	report, err = memoryrolegrants.Run(ctx, pool, memoryrolegrants.Options{OrganizationID: "", Apply: true})
	require.NoError(t, err)
	require.Equal(t, memoryrolegrants.Report{Roles: 0, GrantsAdded: 0}, report)
}

func TestRunNeverReversesLaterRevocation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	orgID := seedOrganization(t, ctx, pool)
	admin := seedLegacySystemRole(t, ctx, pool, orgID, authz.SystemRoleAdmin)

	_, err := memoryrolegrants.Run(ctx, pool, memoryrolegrants.Options{OrganizationID: orgID, Apply: true})
	require.NoError(t, err)
	requireAuthorized(t, ctx, pool, orgID, admin, readCheck, true)

	revokeMemoryGrants(t, ctx, pool, orgID, admin)
	requireAuthorized(t, ctx, pool, orgID, admin, readCheck, false)

	report, err := memoryrolegrants.Run(ctx, pool, memoryrolegrants.Options{OrganizationID: orgID, Apply: true})
	require.NoError(t, err)
	require.Equal(t, memoryrolegrants.Report{Roles: 0, GrantsAdded: 0}, report)
	requireAuthorized(t, ctx, pool, orgID, admin, readCheck, false)
}

func TestRunSkipsRolesRevokedBeforeBackfill(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	orgID := seedOrganization(t, ctx, pool)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, pool, orgID))
	admin := globalRolePrincipal(t, ctx, pool, authz.SystemRoleAdmin)
	revokeMemoryGrants(t, ctx, pool, orgID, admin)

	report, err := memoryrolegrants.Run(ctx, pool, memoryrolegrants.Options{OrganizationID: "", Apply: true})
	require.NoError(t, err)
	require.Equal(t, memoryrolegrants.Report{Roles: 0, GrantsAdded: 0}, report)
	requireAuthorized(t, ctx, pool, orgID, admin, readCheck, false)
	requireAuthorized(t, ctx, pool, orgID, admin, writeCheck, false)
}

func TestRunSkipsRolesWithAnyMemoryGrantOrNoGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)

	seededOrgID := seedOrganization(t, ctx, pool)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, pool, seededOrgID))

	narrowedOrgID := seedOrganization(t, ctx, pool)
	admin := seedLegacySystemRole(t, ctx, pool, narrowedOrgID, authz.SystemRoleAdmin)
	seedGrant(t, ctx, pool, narrowedOrgID, admin, authz.ScopeMemoryRead)

	unseededOrgID := seedOrganization(t, ctx, pool)

	report, err := memoryrolegrants.Run(ctx, pool, memoryrolegrants.Options{OrganizationID: "", Apply: true})
	require.NoError(t, err)
	require.Equal(t, memoryrolegrants.Report{Roles: 0, GrantsAdded: 0}, report)
	requireAuthorized(t, ctx, pool, narrowedOrgID, admin, readCheck, true)
	requireAuthorized(t, ctx, pool, narrowedOrgID, admin, writeCheck, false)
	grants, err := authz.LoadGrants(ctx, pool, unseededOrgID, []urn.Principal{admin})
	require.NoError(t, err)
	require.Empty(t, grants)
}

func TestRunScopesToOrganization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	targetOrgID := seedOrganization(t, ctx, pool)
	otherOrgID := seedOrganization(t, ctx, pool)
	admin := seedLegacySystemRole(t, ctx, pool, targetOrgID, authz.SystemRoleAdmin)
	seedLegacySystemRole(t, ctx, pool, otherOrgID, authz.SystemRoleAdmin)

	report, err := memoryrolegrants.Run(ctx, pool, memoryrolegrants.Options{OrganizationID: targetOrgID, Apply: true})
	require.NoError(t, err)
	require.Equal(t, memoryrolegrants.Report{Roles: 1, GrantsAdded: 2}, report)
	requireAuthorized(t, ctx, pool, targetOrgID, admin, readCheck, true)
	requireAuthorized(t, ctx, pool, otherOrgID, admin, readCheck, false)
}

func seedOrganization(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()

	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(pool).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:       orgID,
		Name:     "Test Org",
		Slug:     orgID,
		WorkosID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	return orgID
}

// seedLegacySystemRole reproduces a built-in role seeded before the memory
// scopes existed: the canonical grant set minus every memory grant, with no
// audit history.
func seedLegacySystemRole(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, slug string) urn.Principal {
	t.Helper()

	require.NoError(t, authz.SeedSystemRoleGrants(ctx, pool, orgID))
	principal := globalRolePrincipal(t, ctx, pool, slug)
	_, err := authz.PatchRoleGrantsTx(ctx, pool, orgID, slug, principal.String(), nil, memoryrolegrants.SystemRoleMemoryGrants(slug))
	require.NoError(t, err)
	requireAuthorized(t, ctx, pool, orgID, principal, readCheck, false)
	return principal
}

// revokeMemoryGrants removes the Admin role's memory grants and records the
// access_role:update audit entry, as RoleManager.UpdateRoleTx does.
func revokeMemoryGrants(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string, principal urn.Principal) {
	t.Helper()

	slug := authz.SystemRoleAdmin
	before, err := authz.PatchRoleGrantsTx(ctx, pool, orgID, slug, principal.String(), nil, nil)
	require.NoError(t, err)
	after, err := authz.PatchRoleGrantsTx(ctx, pool, orgID, slug, principal.String(), nil, memoryrolegrants.SystemRoleMemoryGrants(slug))
	require.NoError(t, err)
	require.Less(t, len(after), len(before))

	err = audit.NewLogger(audit.WithoutOutbox()).LogAccessRoleUpdate(ctx, pool, audit.LogAccessRoleUpdateEvent{
		OrganizationID:     orgID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, "org_admin"),
		ActorDisplayName:   nil,
		ActorSlug:          nil,
		RoleID:             strings.TrimPrefix(principal.ID, "global:"),
		RoleName:           slug,
		RoleSlug:           slug,
		RoleSnapshotBefore: roleSnapshot(principal, slug, before),
		RoleSnapshotAfter:  roleSnapshot(principal, slug, after),
	})
	require.NoError(t, err)
}

func roleSnapshot(principal urn.Principal, slug string, grants []*authz.ScopedGrant) *accessgen.Role {
	view := make([]*accessgen.RoleGrant, 0, len(grants))
	for _, grant := range grants {
		view = append(view, &accessgen.RoleGrant{Scope: grant.Scope, Selectors: nil})
	}
	return &accessgen.Role{
		ID:           strings.TrimPrefix(principal.ID, "global:"),
		PrincipalUrn: principal.String(),
		Name:         slug,
		Slug:         slug,
		Description:  "",
		IsSystem:     true,
		Grants:       view,
		MemberCount:  0,
		AgentIds:     nil,
		CreatedAt:    "",
		UpdatedAt:    "",
	}
}

func globalRolePrincipal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, slug string) urn.Principal {
	t.Helper()

	role, err := accessrepo.New(pool).GetGlobalRoleBySlug(ctx, slug)
	require.NoError(t, err)
	return urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+role.ID.String())
}

func seedGrant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string, principal urn.Principal, scope authz.Scope) {
	t.Helper()

	selectors, err := authz.NewSelector(scope, authz.WildcardResource).MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(pool).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: orgID,
		PrincipalUrn:   principal,
		Scope:          string(scope),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

func requireAuthorized(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string, principal urn.Principal, check authz.Check, want bool) {
	t.Helper()

	grants, err := authz.LoadGrants(ctx, pool, orgID, []urn.Principal{principal})
	require.NoError(t, err)
	allowed, err := authz.GrantsAuthorize(grants, check)
	require.NoError(t, err)
	require.Equal(t, want, allowed, "%s %s", principal.String(), check.Scope)
}

func TestSystemRoleMemoryGrantsCoversAdminOnly(t *testing.T) {
	t.Parallel()

	scopes := make([]string, 0, 2)
	for _, grant := range memoryrolegrants.SystemRoleMemoryGrants(authz.SystemRoleAdmin) {
		require.Nil(t, grant.Selectors)
		scopes = append(scopes, grant.Scope)
	}
	require.ElementsMatch(t, []string{string(authz.ScopeMemoryRead), string(authz.ScopeMemoryWrite)}, scopes)
	require.Empty(t, memoryrolegrants.SystemRoleMemoryGrants(authz.SystemRoleMember))
}

func TestRunIgnoresTemporaryGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)

	// An expired memory grant does not stand in for the permanent ones.
	expiredOrgID := seedOrganization(t, ctx, pool)
	admin := seedLegacySystemRole(t, ctx, pool, expiredOrgID, authz.SystemRoleAdmin)
	seedTemporaryGrant(t, ctx, pool, expiredOrgID, admin, authz.ScopeMemoryRead)

	// A role whose only stored grant is temporary was never provisioned.
	temporaryOnlyOrgID := seedOrganization(t, ctx, pool)
	seedTemporaryGrant(t, ctx, pool, temporaryOnlyOrgID, admin, authz.ScopeProjectRead)

	report, err := memoryrolegrants.Run(ctx, pool, memoryrolegrants.Options{OrganizationID: "", Apply: true})
	require.NoError(t, err)
	require.Equal(t, memoryrolegrants.Report{Roles: 1, GrantsAdded: 2}, report)
	requireAuthorized(t, ctx, pool, expiredOrgID, admin, readCheck, true)
	requireAuthorized(t, ctx, pool, expiredOrgID, admin, writeCheck, true)
	requireAuthorized(t, ctx, pool, temporaryOnlyOrgID, admin, readCheck, false)
}

func seedTemporaryGrant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string, principal urn.Principal, scope authz.Scope) {
	t.Helper()

	selectors, err := authz.NewSelector(scope, authz.WildcardResource).MarshalJSON()
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO principal_grants (organization_id, principal_urn, scope, selectors, expires_at)
VALUES ($1, $2, $3, $4, clock_timestamp() - interval '1 hour')`, orgID, principal.String(), string(scope), selectors)
	require.NoError(t, err)
}
