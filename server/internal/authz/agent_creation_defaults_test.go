package authz

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// seedAdminBeforeCreation models an organization provisioned before creation
// became a distinct scope, without creating an initialization receipt.
func seedAdminBeforeCreation(t *testing.T, db *pgxpool.Pool, org string) urn.Principal {
	t.Helper()
	ctx := t.Context()
	seedOrganization(t, ctx, db, org)
	now := conv.ToPGTimestamptz(time.Now())
	for _, slug := range []string{SystemRoleAdmin, SystemRoleMember} {
		require.NoError(t, accessrepo.New(db).UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{
			WorkosSlug: slug, WorkosName: slug, WorkosDescription: conv.ToPGTextEmpty("System role"),
			WorkosCreatedAt: now, WorkosUpdatedAt: now, WorkosLastEventID: conv.ToPGTextEmpty(""),
		}))
	}
	role, err := accessrepo.New(db).GetGlobalRoleBySlug(ctx, SystemRoleAdmin)
	require.NoError(t, err)
	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+role.ID.String())
	seedGrant(t, ctx, db, org, principal, ScopeOrgAdmin, WildcardResource)
	return principal
}

func TestAdminCreationInitializationIsOnceEvenAfterRemoval(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := newTestDB(t)
	principal := seedAdminBeforeCreation(t, db, "org-initialize")
	member, err := accessrepo.New(db).GetGlobalRoleBySlug(ctx, SystemRoleMember)
	require.NoError(t, err)
	untouched := []urn.Principal{
		urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+member.ID.String()),
		urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+uuid.NewString()),
	}
	for _, role := range untouched {
		seedGrant(t, ctx, db, "org-initialize", role, ScopeMCPConnect, WildcardResource)
	}
	require.NoError(t, pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		added, err := InitializeAdminAgentCreationTx(ctx, tx, "org-initialize")
		require.NoError(t, err)
		require.True(t, added)
		return nil
	}))
	grants, err := LoadGrants(ctx, db, "org-initialize", []urn.Principal{principal})
	require.NoError(t, err)
	require.True(t, GrantsSatisfy(grants, AgentCreationChecks("caller", "owner")[0]))
	require.NoError(t, pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		_, err := PatchRoleGrantsTx(ctx, tx, "org-initialize", SystemRoleAdmin, principal.String(), nil, []*RoleGrant{{Scope: string(ScopeAgentCreate)}})
		return err
	}))
	require.NoError(t, pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		added, err := InitializeAdminAgentCreationTx(ctx, tx, "org-initialize")
		require.NoError(t, err)
		require.False(t, added)
		return nil
	}))
	grants, err = LoadGrants(ctx, db, "org-initialize", []urn.Principal{principal})
	require.NoError(t, err)
	require.False(t, GrantsSatisfy(grants, AgentCreationChecks("caller", "owner")[0]))
	for _, role := range untouched {
		grants, err := LoadGrants(ctx, db, "org-initialize", []urn.Principal{role})
		require.NoError(t, err)
		require.False(t, GrantsSatisfy(grants, AgentCreationChecks("caller", "owner")[0]))
		stored, err := accessrepo.New(db).GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{OrganizationID: "org-initialize", PrincipalUrns: []string{role.String()}})
		require.NoError(t, err)
		require.Len(t, stored, 1)
		require.Equal(t, string(ScopeMCPConnect), stored[0].Scope)
	}
	count, err := audittest.AuditLogCountByAction(ctx, db, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestNewOrganizationCreationSeedCompletesInitialization(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := newTestDB(t)
	seedOrganization(t, ctx, db, "org-new")
	require.NoError(t, SeedSystemRoleGrants(ctx, db, "org-new"))
	role, err := accessrepo.New(db).GetGlobalRoleBySlug(ctx, SystemRoleAdmin)
	require.NoError(t, err)
	principal := urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+role.ID.String())
	require.NoError(t, pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		_, err := PatchRoleGrantsTx(ctx, tx, "org-new", SystemRoleAdmin, principal.String(), nil, []*RoleGrant{{Scope: string(ScopeAgentCreate)}})
		return err
	}))
	require.NoError(t, SeedSystemRoleGrants(ctx, db, "org-new"))
	require.NoError(t, pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		added, err := InitializeAdminAgentCreationTx(ctx, tx, "org-new")
		require.NoError(t, err)
		require.False(t, added)
		return nil
	}))
	grants, err := LoadGrants(ctx, db, "org-new", []urn.Principal{principal})
	require.NoError(t, err)
	require.False(t, GrantsSatisfy(grants, AgentCreationChecks("caller", "owner")[0]))
}

func TestAdminCreationDryRunRollsBackGrantAndReceipt(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := newTestDB(t)
	principal := seedAdminBeforeCreation(t, db, "org-dry-run")
	tx := testenv.BeginTx(t, ctx, db)
	added, err := InitializeAdminAgentCreationTx(ctx, tx, "org-dry-run")
	require.NoError(t, err)
	require.True(t, added)
	require.NoError(t, tx.Rollback(ctx))
	grants, err := LoadGrants(ctx, db, "org-dry-run", []urn.Principal{principal})
	require.NoError(t, err)
	require.False(t, GrantsSatisfy(grants, AgentCreationChecks("caller", "owner")[0]))
	require.NoError(t, pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		added, err := InitializeAdminAgentCreationTx(ctx, tx, "org-dry-run")
		require.NoError(t, err)
		require.True(t, added, "dry run must not leave a completion receipt")
		return nil
	}))
}

func TestAdminCreationInitializationSerializesWithGrantRemoval(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := newTestDB(t)
	principal := seedAdminBeforeCreation(t, db, "org-race")
	tx := testenv.BeginTx(t, ctx, db)
	added, err := InitializeAdminAgentCreationTx(ctx, tx, "org-race")
	require.NoError(t, err)
	require.True(t, added)
	done := make(chan error, 1)
	go func() {
		done <- pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
			_, err := PatchRoleGrantsTx(ctx, tx, "org-race", SystemRoleAdmin, principal.String(), nil, []*RoleGrant{{Scope: string(ScopeAgentCreate)}})
			return err
		})
	}()
	testenv.WaitForQueryBlockedBy(t, ctx, db, testenv.BackendPID(tx), "%LockOrganizationForAdminConfiguration%")
	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, <-done)
	require.NoError(t, pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		added, err := InitializeAdminAgentCreationTx(ctx, tx, "org-race")
		require.NoError(t, err)
		require.False(t, added)
		return nil
	}))
	grants, err := LoadGrants(ctx, db, "org-race", []urn.Principal{principal})
	require.NoError(t, err)
	require.False(t, GrantsSatisfy(grants, AgentCreationChecks("caller", "owner")[0]))
}

func TestAdminCreationInitializationRejectsInactiveAdmin(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db := newTestDB(t)
	principal := seedAdminBeforeCreation(t, db, "org-inactive-admin")
	_, err := accessrepo.New(db).MarkGlobalRoleDeleted(ctx, accessrepo.MarkGlobalRoleDeletedParams{
		WorkosSlug: SystemRoleAdmin, WorkosDeletedAt: conv.ToPGTimestamptz(time.Now()), WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		_, err := InitializeAdminAgentCreationTx(ctx, tx, "org-inactive-admin")
		return err
	})
	require.ErrorContains(t, err, "global Admin role is inactive")
	rows, err := accessrepo.New(db).GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{OrganizationID: "org-inactive-admin", PrincipalUrns: []string{principal.String()}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, string(ScopeOrgAdmin), rows[0].Scope)
	count, err := audittest.AuditLogCountByAction(ctx, db, audit.ActionAccessRoleUpdate)
	require.NoError(t, err)
	require.Zero(t, count)
}
