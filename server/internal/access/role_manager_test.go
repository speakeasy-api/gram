package access

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestRoleManager_ListRoles(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	adminID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	customID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Can build"))

	roles, err := ti.service.roleMgr.ListRoles(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Len(t, roles.Roles, 2)

	bySlug := map[string]string{}
	for _, role := range roles.Roles {
		if role.Name == "Admin" {
			bySlug["admin"] = role.ID
		}
		if role.Name == "Custom Builder" {
			bySlug["custom-builder"] = role.ID
		}
	}
	require.Equal(t, adminID, bySlug["admin"])
	require.Equal(t, customID, bySlug["custom-builder"])
}

func TestRoleManager_GetRoleByID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	customID := seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Can build"))

	role, err := ti.service.roleMgr.GetRoleByID(ctx, authCtx.ActiveOrganizationID, customID)
	require.NoError(t, err)
	require.Equal(t, customID, role.ID)
	require.Equal(t, "Custom Builder", role.Name)

	_, err = ti.service.roleMgr.GetRoleByID(ctx, authCtx.ActiveOrganizationID, "not-a-uuid")
	require.Error(t, err)
}

func TestRoleManager_MembersAndCounts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_admin", "Admin", "admin"))
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Can build"))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "u1@example.com", "User 1", "user_1", "membership_1")
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", "u2@example.com", "User 2", "user_2", "membership_2")
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", mockMember(mockidp.MockOrgID, "membership_1", "user_1", "admin"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_2", mockMember(mockidp.MockOrgID, "membership_2", "user_2", "custom-builder"))
	seedRoleAssignment(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "", mockMember(mockidp.MockOrgID, "membership_3", "user_3", "custom-builder"))

	manager := ti.service.roleMgr
	members, err := manager.ListMembers(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Len(t, members.Members, 3)

	rolePrincipals, err := manager.MemberRolePrincipals(ctx, authCtx.ActiveOrganizationID, "user_2")
	require.NoError(t, err)
	slugs := make([]string, 0, len(rolePrincipals))
	for _, role := range rolePrincipals {
		slugs = append(slugs, role.RoleSlug)
	}
	require.Equal(t, []string{"custom-builder"}, slugs)

	roles, err := manager.ListRoles(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	counts := make(map[string]int, len(roles.Roles))
	for _, role := range roles.Roles {
		counts[role.Name] = role.MemberCount
	}
	require.Equal(t, 1, counts["Admin"])
	require.Equal(t, 1, counts["Custom Builder"])
}

func TestRoleManager_AssignMembersToRoleAcceptsConnectedMemberWithoutAssignment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Can build"))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "u1@example.com", "User 1", "user_1", "membership_1")

	assigned, _, err := ti.service.roleMgr.assignMembersToRoleTx(ctx, ti.conn, authCtx.ActiveOrganizationID, "custom-builder", []string{"local_user_1"})
	require.NoError(t, err)
	require.Equal(t, 1, assigned)
}

func TestRoleManager_ReconcileMemberRolesAllowsEmptyDesiredState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "u1@example.test", "User 1", "user_1", "membership_1")
	tx := testenv.BeginTx(t, ctx, ti.conn)
	defer func() { _ = tx.Rollback(ctx) }()
	reconciliation, err := ti.service.roleMgr.CurrentMemberRoleReconciliationTx(ctx, tx, authCtx.ActiveOrganizationID, "local_user_1")
	require.NoError(t, err)
	require.Equal(t, "membership_1", reconciliation.membershipID)
	require.Equal(t, authCtx.ActiveOrganizationID, reconciliation.organizationID)
	require.Equal(t, "user_1", reconciliation.workosUserID)
	require.NoError(t, tx.Commit(ctx))

	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", []string{}).Return(nil, nil).Once()
	ti.service.roleMgr.ReconcileMemberRoles(ctx, reconciliation)
	ti.service.roleMgr.ReconcileMemberRoles(ctx, MemberRoleReconciliation{})
}

func TestRoleManager_DelayedMemberRoleTargetReadsCurrentState(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_old", "Old", "old", "Old role"))
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_new", "New", "new", "New role"))
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_user_1", "u1@example.test", "User 1", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, orgID, "local_user_1", mockMember(mockidp.MockOrgID, "membership_1", "user_1", "old"))
	tx := testenv.BeginTx(t, ctx, ti.conn)
	target, err := ti.service.roleMgr.CurrentMemberRoleReconciliationTx(ctx, tx, orgID, "local_user_1")
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))

	_, err = accessrepo.New(ti.conn).SoftDeleteAllRoleAssignmentsByWorkosUser(ctx, accessrepo.SoftDeleteAllRoleAssignmentsByWorkosUserParams{OrganizationID: orgID, WorkosUserID: "user_1"})
	require.NoError(t, err)
	seedRoleAssignment(t, ctx, ti.conn, orgID, "local_user_1", mockMember(mockidp.MockOrgID, "membership_1", "user_1", "new"))
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", []string{"new"}).Return(nil, nil).Once()
	ti.service.roleMgr.memberRoleSync(target)(ctx)

	_, err = accessrepo.New(ti.conn).SoftDeleteAllRoleAssignmentsByWorkosUser(ctx, accessrepo.SoftDeleteAllRoleAssignmentsByWorkosUserParams{OrganizationID: orgID, WorkosUserID: "user_1"})
	require.NoError(t, err)
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", []string{}).Return(nil, nil).Once()
	ti.service.roleMgr.memberRoleSync(target)(ctx)
}

func TestRoleManager_MemberRoleSyncSerializesManagersAndLocksMember(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"connected", "legacy"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAccessService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			orgID := authCtx.ActiveOrganizationID
			seedRole(t, ctx, ti.conn, orgID, mockRole("role_custom", "Custom", "custom", "Custom role"))
			if name == "connected" {
				seedConnectedUser(t, ctx, ti.conn, orgID, "local_user_1", "u1@example.test", "User 1", "user_1", "membership_1")
			}
			seedRoleAssignment(t, ctx, ti.conn, orgID, "", mockMember(mockidp.MockOrgID, "membership_1", "user_1", "custom"))
			target := MemberRoleReconciliation{organizationID: orgID, workosUserID: "user_1", membershipID: "membership_1"}
			manager := ti.service.roleMgr
			other := NewRoleManager(manager.logger, ti.conn, ti.roles, manager.audit)
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			t.Cleanup(func() {
				close(release)
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("member sync did not release its transaction")
				}
			})
			ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", []string{"custom"}).Run(func(args mock.Arguments) {
				close(entered)
				callCtx, ok := args.Get(0).(context.Context)
				if !ok {
					t.Error("provider call context is missing")
					return
				}
				select {
				case <-release:
				case <-callCtx.Done():
				}
			}).Return(nil, nil).Once()
			go func() {
				syncCtx, cancel := context.WithTimeout(ctx, workOSSyncTimeout)
				defer cancel()
				manager.memberRoleSync(target)(syncCtx)
				close(done)
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("first provider send did not start")
			}

			waitCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancel()
			other.memberRoleSync(target)(waitCtx)
			require.ErrorIs(t, waitCtx.Err(), context.DeadlineExceeded)
			ti.roles.AssertNumberOfCalls(t, "UpdateMemberRoles", 1)

			if name == "legacy" {
				return
			}
			tx := testenv.BeginTx(t, ctx, ti.conn)
			defer func() { _ = tx.Rollback(ctx) }()
			lockCtx, cancelLock := context.WithTimeout(ctx, 100*time.Millisecond)
			defer cancelLock()
			_, err := accessrepo.New(tx).LockOrganizationUserRelationship(lockCtx, accessrepo.LockOrganizationUserRelationshipParams{OrganizationID: orgID, UserID: "local_user_1"})
			require.Error(t, err)
			require.ErrorIs(t, lockCtx.Err(), context.DeadlineExceeded)
		})
	}
}

func TestRoleManager_MemberRoleSyncLegacyTargetAndChangedMembership(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_custom", "Custom", "custom", "Custom role"))
	seedRoleAssignment(t, ctx, ti.conn, orgID, "", mockMember(mockidp.MockOrgID, "membership_1", "user_1", "custom"))
	target := MemberRoleReconciliation{organizationID: orgID, workosUserID: "user_1", membershipID: "membership_1"}
	ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_1", []string{"custom"}).Return(nil, nil).Once()
	ti.service.roleMgr.memberRoleSync(target)(ctx)

	seedConnectedUser(t, ctx, ti.conn, orgID, "local_user_1", "u1@example.test", "User 1", "user_1", "membership_replaced")
	ti.service.roleMgr.memberRoleSync(target)(ctx)
	ti.roles.AssertNumberOfCalls(t, "UpdateMemberRoles", 1)
}

func TestRoleManager_AddMemberRoleRepairsNullLinkWithoutReplacingRoles(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	roleID := seedRole(t, ctx, ti.conn, orgID, mockRole("role_custom", "Custom", "custom", "Custom role"))
	otherID := seedRole(t, ctx, ti.conn, orgID, mockRole("role_other", "Other", "other", "Other role"))
	seedConnectedUser(t, ctx, ti.conn, orgID, "local_user_1", "u1@example.test", "User 1", "user_1", "membership_1")
	seedRoleAssignment(t, ctx, ti.conn, orgID, "", mockMember(mockidp.MockOrgID, "membership_1", "user_1", "custom"))
	seedRoleAssignment(t, ctx, ti.conn, orgID, "local_user_1", mockMember(mockidp.MockOrgID, "membership_1", "user_1", "other"))
	queries := accessrepo.New(ti.conn)
	beforeRecords, err := queries.ListOrganizationRoleAssignmentRecordsByWorkosUser(ctx, accessrepo.ListOrganizationRoleAssignmentRecordsByWorkosUserParams{OrganizationID: orgID, WorkosUserID: "user_1"})
	require.NoError(t, err)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	actor := RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, "test_actor")}
	for _, changed := range []bool{true, false} {
		tx := testenv.BeginTx(t, ctx, ti.conn)
		result, _, err := ti.service.roleMgr.AddMemberRoleTx(ctx, tx, orgID, "local_user_1", roleID, actor, nil)
		require.NoError(t, err)
		require.Equal(t, changed, result.Changed)
		require.ElementsMatch(t, []string{roleID, otherID}, result.After.RoleIDs)
		require.NoError(t, tx.Commit(ctx))
	}
	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAccessMemberRoleUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
	afterRecords, err := queries.ListOrganizationRoleAssignmentRecordsByWorkosUser(ctx, accessrepo.ListOrganizationRoleAssignmentRecordsByWorkosUserParams{OrganizationID: orgID, WorkosUserID: "user_1"})
	require.NoError(t, err)
	require.Len(t, afterRecords, len(beforeRecords))
	for i, record := range afterRecords {
		require.Equal(t, "local_user_1", record.UserID.String)
		require.Equal(t, beforeRecords[i].ID, record.ID)
		require.Equal(t, beforeRecords[i].WorkosUpdatedAt, record.WorkosUpdatedAt)
		require.Equal(t, beforeRecords[i].WorkosLastEventID, record.WorkosLastEventID)
		require.False(t, record.DeletedAt.Valid)
	}
	roles, err := queries.ListMemberRolePrincipalsByUser(ctx, accessrepo.ListMemberRolePrincipalsByUserParams{OrganizationID: orgID, UserID: "local_user_1"})
	require.NoError(t, err)
	require.Len(t, roles, 2)
}

// Target resolution uses Query; the first QueryRow is the relationship lock.
// Interleave a committed membership change at that exact boundary.
type memberAssignmentRaceTx struct {
	pgx.Tx
	once       sync.Once
	beforeLock func()
}

func (tx *memberAssignmentRaceTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	tx.once.Do(tx.beforeLock)
	return tx.Tx.QueryRow(ctx, sql, args...) //nolint:glint // forwards SQLc SQL while synchronizing a membership-change regression
}

func TestRoleManager_BulkAssignmentRevalidatesLockedMembership(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"deleted membership", "deleted user", "new membership"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAccessService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			orgID := authCtx.ActiveOrganizationID
			seedRole(t, ctx, ti.conn, orgID, mockRole("role_custom", "Custom", "custom", ""))
			seedConnectedUser(t, ctx, ti.conn, orgID, "local_user_1", "u1@example.test", "User 1", "user_1", "membership_1")
			tx := testenv.BeginTx(t, ctx, ti.conn)
			defer func() { _ = tx.Rollback(ctx) }()
			raceTx := &memberAssignmentRaceTx{Tx: tx, beforeLock: func() {
				switch change {
				case "deleted membership":
					require.NoError(t, testrepo.New(ti.conn).ForceSoftDeleteOrganizationUserRelationshipsFixture(ctx, orgID))
				case "deleted user":
					require.NoError(t, testrepo.New(ti.conn).ForceSoftDeleteUser(ctx, "local_user_1"))
				case "new membership":
					require.NoError(t, orgrepo.New(ti.conn).UpsertWorkOSMembership(ctx, orgrepo.UpsertWorkOSMembershipParams{
						OrganizationID: orgID, UserID: conv.ToPGText("local_user_1"), WorkosUserID: conv.ToPGText("user_1"),
						WorkosMembershipID: conv.ToPGText("membership_2"), WorkosUpdatedAt: conv.ToPGTimestamptz(time.Now()), WorkosLastEventID: conv.ToPGText("event_fixture"),
					}))
				}
			}}
			assigned, syncs, err := ti.service.roleMgr.assignMembersToRoleTx(ctx, raceTx, orgID, "custom", []string{"local_user_1"})
			if change != "new membership" {
				require.Error(t, err)
				require.Zero(t, assigned)
				require.Empty(t, syncs)
				records, readErr := accessrepo.New(tx).ListOrganizationRoleAssignmentRecordsByWorkosUser(ctx, accessrepo.ListOrganizationRoleAssignmentRecordsByWorkosUserParams{OrganizationID: orgID, WorkosUserID: "user_1"})
				require.NoError(t, readErr)
				require.Empty(t, records)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, assigned)
			records, err := accessrepo.New(tx).ListOrganizationRoleAssignmentRecordsByWorkosUser(ctx, accessrepo.ListOrganizationRoleAssignmentRecordsByWorkosUserParams{OrganizationID: orgID, WorkosUserID: "user_1"})
			require.NoError(t, err)
			require.Len(t, records, 1)
			require.Equal(t, "membership_2", records[0].WorkosMembershipID.String)
			require.Equal(t, "local_user_1", records[0].UserID.String)
			require.NoError(t, tx.Commit(ctx))
			ti.roles.On("UpdateMemberRoles", mock.Anything, "membership_2", []string{"custom"}).Return(nil, nil).Once()
			for _, send := range syncs {
				send(ctx)
			}
		})
	}
}

func TestRoleManager_LegacyDeletedAssignmentsDoNotAuthorizeSync(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_custom", "Custom", "custom", ""))
	seedRoleAssignment(t, ctx, ti.conn, orgID, "", mockMember(mockidp.MockOrgID, "membership_1", "user_1", "custom"))
	_, err := accessrepo.New(ti.conn).SoftDeleteAllRoleAssignmentsByWorkosUser(ctx, accessrepo.SoftDeleteAllRoleAssignmentsByWorkosUserParams{OrganizationID: orgID, WorkosUserID: "user_1"})
	require.NoError(t, err)
	ti.service.roleMgr.memberRoleSync(MemberRoleReconciliation{organizationID: orgID, workosUserID: "user_1", membershipID: "membership_1"})(ctx)
	ti.roles.AssertNotCalled(t, "UpdateMemberRoles", mock.Anything, mock.Anything, mock.Anything)
}

func TestRoleManager_RunWorkOSSyncsDetachesCancellationWithDeadline(t *testing.T) {
	t.Parallel()

	parent, cancel := context.WithCancel(t.Context())
	cancel()

	type syncContext struct {
		err         error
		hasDeadline bool
		remaining   time.Duration
	}
	observed := make(chan syncContext, 1)
	(&RoleManager{}).runWorkOSSyncs(parent, []workosSync{func(ctx context.Context) {
		deadline, hasDeadline := ctx.Deadline()
		observed <- syncContext{err: ctx.Err(), hasDeadline: hasDeadline, remaining: time.Until(deadline)}
	}})

	select {
	case got := <-observed:
		require.NoError(t, got.err)
		require.True(t, got.hasDeadline)
		require.Positive(t, got.remaining)
		require.LessOrEqual(t, got.remaining, workOSSyncTimeout)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for detached WorkOS sync")
	}
}

func TestRoleManager_LocalRoleWritePreservesWorkOSLastEventID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	now := time.Now().UTC()
	_, err := accessrepo.New(ti.conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    authCtx.ActiveOrganizationID,
		WorkosSlug:        "custom-builder",
		WorkosName:        "Custom Builder",
		WorkosDescription: conv.ToPGTextEmpty("Before"),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGText("event_01SEED"),
	})
	require.NoError(t, err)

	_, err = accessrepo.New(ti.conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    authCtx.ActiveOrganizationID,
		WorkosSlug:        "custom-builder",
		WorkosName:        "Custom Builder",
		WorkosDescription: conv.ToPGTextEmpty("After"),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now.Add(time.Minute)),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	row, err := accessrepo.New(ti.conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		WorkosSlug:     "custom-builder",
	})
	require.NoError(t, err)
	require.Equal(t, "event_01SEED", row.WorkosLastEventID.String)

	replaced, err := accessrepo.New(ti.conn).ReplaceOrganizationRoleAssignment(ctx, accessrepo.ReplaceOrganizationRoleAssignmentParams{
		OrganizationID:     authCtx.ActiveOrganizationID,
		WorkosUserID:       "user_1",
		WorkosRoleSlug:     "custom-builder",
		UserID:             conv.ToPGTextEmpty(""),
		WorkosMembershipID: conv.ToPGTextEmpty("membership_1"),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(now),
		WorkosLastEventID:  conv.ToPGText("event_02SEED"),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), replaced)

	replaced, err = accessrepo.New(ti.conn).ReplaceOrganizationRoleAssignment(ctx, accessrepo.ReplaceOrganizationRoleAssignmentParams{
		OrganizationID:     authCtx.ActiveOrganizationID,
		WorkosUserID:       "user_1",
		WorkosRoleSlug:     "custom-builder",
		UserID:             conv.ToPGTextEmpty(""),
		WorkosMembershipID: conv.ToPGTextEmpty("membership_1"),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(now.Add(time.Minute)),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), replaced)

	assignments, err := accessrepo.New(ti.conn).ListOrganizationRoleAssignmentRecordsByWorkosUser(ctx, accessrepo.ListOrganizationRoleAssignmentRecordsByWorkosUserParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		WorkosUserID:   "user_1",
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.False(t, assignments[0].DeletedAt.Valid)
	require.Equal(t, "event_02SEED", assignments[0].WorkosLastEventID.String)
}

func TestRoleManager_ReplaceRoleAssignmentSoftDeletesPreviousRole(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockSystemRole("role_member", "Member", "member"))
	seedRole(t, ctx, ti.conn, authCtx.ActiveOrganizationID, mockRole("role_custom", "Custom Builder", "custom-builder", "Can build"))
	seedConnectedUser(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "local_user_1", "u1@example.com", "User 1", "user_1", "membership_1")

	// Use ReplaceOrganizationRoleAssignment directly to test its upsert-one + soft-delete-others behavior.
	now := time.Now().UTC()
	replaced, err := accessrepo.New(ti.conn).ReplaceOrganizationRoleAssignment(ctx, accessrepo.ReplaceOrganizationRoleAssignmentParams{
		OrganizationID:     authCtx.ActiveOrganizationID,
		WorkosUserID:       "user_1",
		WorkosRoleSlug:     "member",
		UserID:             conv.ToPGTextEmpty("local_user_1"),
		WorkosMembershipID: conv.ToPGTextEmpty("membership_1"),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(now),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), replaced)

	replaced, err = accessrepo.New(ti.conn).ReplaceOrganizationRoleAssignment(ctx, accessrepo.ReplaceOrganizationRoleAssignmentParams{
		OrganizationID:     authCtx.ActiveOrganizationID,
		WorkosUserID:       "user_1",
		WorkosRoleSlug:     "custom-builder",
		UserID:             conv.ToPGTextEmpty("local_user_1"),
		WorkosMembershipID: conv.ToPGTextEmpty("membership_1"),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(now),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), replaced)

	assignments, err := accessrepo.New(ti.conn).ListOrganizationRoleAssignmentRecordsByWorkosUser(ctx, accessrepo.ListOrganizationRoleAssignmentRecordsByWorkosUserParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		WorkosUserID:   "user_1",
	})
	require.NoError(t, err)
	activeCount := 0
	deletedCount := 0
	for _, assignment := range assignments {
		if assignment.DeletedAt.Valid {
			deletedCount++
			continue
		}
		activeCount++
	}
	require.Equal(t, 1, activeCount)
	require.Equal(t, 1, deletedCount)
}
