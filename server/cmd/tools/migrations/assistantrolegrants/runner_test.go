package assistantrolegrants_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/assistantrolegrants"
	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var (
	readCheck  = authz.AssistantCheck(authz.ScopeAssistantRead, "assistant_1", "project_1")
	writeCheck = authz.AssistantCheck(authz.ScopeAssistantWrite, "assistant_1", "project_1")
)

func TestRunBackfillsLegacySystemRolesOnce(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	orgID := seedOrganization(t, ctx, pool)
	admin := seedLegacySystemRole(t, ctx, pool, orgID, authz.SystemRoleAdmin)
	member := seedLegacySystemRole(t, ctx, pool, orgID, authz.SystemRoleMember)
	custom := urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+uuid.NewString())
	seedGrant(t, ctx, pool, orgID, custom, authz.ScopeProjectRead)

	report, err := assistantrolegrants.Run(ctx, pool, assistantrolegrants.Options{OrganizationID: "", Apply: false})
	require.NoError(t, err)
	require.Equal(t, assistantrolegrants.Report{Roles: 2, GrantsAdded: 3}, report)
	requireAuthorized(t, ctx, pool, orgID, member, readCheck, false)
	requireAuthorized(t, ctx, pool, orgID, admin, readCheck, false)

	report, err = assistantrolegrants.Run(ctx, pool, assistantrolegrants.Options{OrganizationID: "", Apply: true})
	require.NoError(t, err)
	require.Equal(t, assistantrolegrants.Report{Roles: 2, GrantsAdded: 3}, report)
	requireAuthorized(t, ctx, pool, orgID, member, readCheck, true)
	requireAuthorized(t, ctx, pool, orgID, member, writeCheck, false)
	requireAuthorized(t, ctx, pool, orgID, admin, readCheck, true)
	requireAuthorized(t, ctx, pool, orgID, admin, writeCheck, true)
	requireAuthorized(t, ctx, pool, orgID, custom, readCheck, false)

	report, err = assistantrolegrants.Run(ctx, pool, assistantrolegrants.Options{OrganizationID: "", Apply: true})
	require.NoError(t, err)
	require.Equal(t, assistantrolegrants.Report{Roles: 0, GrantsAdded: 0}, report)
}

func TestRunNeverReversesLaterRevocation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	orgID := seedOrganization(t, ctx, pool)
	member := seedLegacySystemRole(t, ctx, pool, orgID, authz.SystemRoleMember)

	_, err := assistantrolegrants.Run(ctx, pool, assistantrolegrants.Options{OrganizationID: orgID, Apply: true})
	require.NoError(t, err)
	requireAuthorized(t, ctx, pool, orgID, member, readCheck, true)

	revokeAssistantGrants(t, ctx, pool, orgID, authz.SystemRoleMember, member)
	requireAuthorized(t, ctx, pool, orgID, member, readCheck, false)

	report, err := assistantrolegrants.Run(ctx, pool, assistantrolegrants.Options{OrganizationID: orgID, Apply: true})
	require.NoError(t, err)
	require.Equal(t, assistantrolegrants.Report{Roles: 0, GrantsAdded: 0}, report)
	requireAuthorized(t, ctx, pool, orgID, member, readCheck, false)
}

func TestRunSkipsRolesRevokedBeforeBackfill(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	orgID := seedOrganization(t, ctx, pool)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, pool, orgID))
	admin := globalRolePrincipal(t, ctx, pool, authz.SystemRoleAdmin)
	revokeAssistantGrants(t, ctx, pool, orgID, authz.SystemRoleAdmin, admin)

	report, err := assistantrolegrants.Run(ctx, pool, assistantrolegrants.Options{OrganizationID: "", Apply: true})
	require.NoError(t, err)
	require.Equal(t, assistantrolegrants.Report{Roles: 0, GrantsAdded: 0}, report)
	requireAuthorized(t, ctx, pool, orgID, admin, readCheck, false)
	requireAuthorized(t, ctx, pool, orgID, admin, writeCheck, false)
}

func TestRunSkipsRolesWithAnyAssistantGrantOrNoGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)

	seededOrgID := seedOrganization(t, ctx, pool)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, pool, seededOrgID))

	narrowedOrgID := seedOrganization(t, ctx, pool)
	member := seedLegacySystemRole(t, ctx, pool, narrowedOrgID, authz.SystemRoleMember)
	seedGrant(t, ctx, pool, narrowedOrgID, member, authz.ScopeAssistantBlockedRead)

	unseededOrgID := seedOrganization(t, ctx, pool)

	report, err := assistantrolegrants.Run(ctx, pool, assistantrolegrants.Options{OrganizationID: "", Apply: true})
	require.NoError(t, err)
	require.Equal(t, assistantrolegrants.Report{Roles: 0, GrantsAdded: 0}, report)
	requireAuthorized(t, ctx, pool, narrowedOrgID, member, readCheck, false)
	grants, err := authz.LoadGrants(ctx, pool, unseededOrgID, []urn.Principal{member})
	require.NoError(t, err)
	require.Empty(t, grants)
}

func TestRunScopesToOrganization(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := newTestDB(t)
	targetOrgID := seedOrganization(t, ctx, pool)
	otherOrgID := seedOrganization(t, ctx, pool)
	member := seedLegacySystemRole(t, ctx, pool, targetOrgID, authz.SystemRoleMember)
	seedLegacySystemRole(t, ctx, pool, otherOrgID, authz.SystemRoleMember)

	report, err := assistantrolegrants.Run(ctx, pool, assistantrolegrants.Options{OrganizationID: targetOrgID, Apply: true})
	require.NoError(t, err)
	require.Equal(t, assistantrolegrants.Report{Roles: 1, GrantsAdded: 1}, report)
	requireAuthorized(t, ctx, pool, targetOrgID, member, readCheck, true)
	requireAuthorized(t, ctx, pool, otherOrgID, member, readCheck, false)
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

// seedLegacySystemRole reproduces a built-in role seeded before the assistant
// scopes existed: the canonical grant set minus every assistant grant, with
// no audit history.
func seedLegacySystemRole(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, slug string) urn.Principal {
	t.Helper()

	require.NoError(t, authz.SeedSystemRoleGrants(ctx, pool, orgID))
	principal := globalRolePrincipal(t, ctx, pool, slug)
	_, err := authz.PatchRoleGrantsTx(ctx, pool, orgID, slug, principal.String(), nil, authz.SystemRoleAssistantGrants(slug))
	require.NoError(t, err)
	requireAuthorized(t, ctx, pool, orgID, principal, readCheck, false)
	return principal
}

// revokeAssistantGrants removes a role's assistant grants and records the
// access_role:update audit entry, as RoleManager.UpdateRoleTx does.
func revokeAssistantGrants(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, slug string, principal urn.Principal) {
	t.Helper()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := authz.PatchRoleGrantsTx(ctx, tx, orgID, slug, principal.String(), nil, nil)
	require.NoError(t, err)
	after, err := authz.PatchRoleGrantsTx(ctx, tx, orgID, slug, principal.String(), nil, authz.SystemRoleAssistantGrants(slug))
	require.NoError(t, err)
	require.Less(t, len(after), len(before))

	err = audit.NewLogger(audit.WithoutOutbox()).LogAccessRoleUpdate(ctx, tx, audit.LogAccessRoleUpdateEvent{
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
	require.NoError(t, tx.Commit(ctx))
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
