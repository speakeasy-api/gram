package access

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func directoryInventorySupportContext(t *testing.T, ctx context.Context, ti *testInstance) context.Context {
	t.Helper()
	ac := *testAccessAuthContext(t, ctx)
	require.NoError(t, testrepo.New(ti.conn).SetUserPlatformAdminFixture(ctx, testrepo.SetUserPlatformAdminFixtureParams{ID: ac.UserID, Admin: true}))
	ac.IsAdmin = true
	ac.SupportOrganizationID = ac.ActiveOrganizationID
	prepared, err := ti.service.authz.PrepareContext(contextvalues.WithValidatedSupportSession(ctx, &ac))
	require.NoError(t, err)
	return prepared
}

func directoryInventoryFixture(t *testing.T, ctx context.Context, ti *testInstance, groupID, slug string) DirectoryRoleInventory {
	t.Helper()
	ac := testAccessAuthContext(t, ctx)
	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	return DirectoryRoleInventory{
		OrganizationID: ac.ActiveOrganizationID, WorkOSOrganizationID: org.WorkosID.String, DefaultRoleSlug: "member",
		Assignments: []DirectoryRoleInventoryAssignment{{WorkOSGroupID: groupID, RoleSlug: slug}},
	}
}

func TestDirectoryRoleInventoryImportPreservesMappingsAndIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ctx = directoryInventorySupportContext(t, ctx, ti)
	enableDirectoryRoleSetsForTest(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_viewer", "Viewer", "viewer", ""))
	group, workosGroup := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")
	_, otherGroup := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Support")
	viewer := seededRolePrincipal(t, ctx, ti.conn, orgID, "viewer").String()
	existing, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{SourceKind: "group", DirectoryGroupID: new(group.String()), RoleUrns: []string{viewer}})
	require.NoError(t, err)
	baseline, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	inventory := directoryInventoryFixture(t, ctx, ti, workosGroup, "builder")
	inventory.Assignments = append(inventory.Assignments, inventory.Assignments[0], DirectoryRoleInventoryAssignment{WorkOSGroupID: otherGroup, RoleSlug: "viewer"})
	report, err := RunDirectoryRoleInventory(ctx, testenv.NewLogger(t), ti.conn, ti.service.authz, ti.service.audit, inventory, false, true)
	require.NoError(t, err)
	require.True(t, report.Committed)
	require.Equal(t, 2, report.AddedMappings)
	mappings, err := repo.New(ti.conn).ListDirectoryRoleMappings(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, mappings, 3)
	require.Contains(t, []string{mappings[0].ID.String(), mappings[1].ID.String(), mappings[2].ID.String()}, existing[0].ID)
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	require.Equal(t, baseline+2, count)
	auditEntry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	require.Equal(t, testAccessAuthContext(t, ctx).UserID, auditEntry.ActorID)
	report, err = RunDirectoryRoleInventory(ctx, testenv.NewLogger(t), ti.conn, ti.service.authz, ti.service.audit, inventory, false, true)
	require.NoError(t, err)
	require.Zero(t, report.AddedMappings)
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	require.Equal(t, count, after)
}

func TestDirectoryRoleInventoryImportDryRunDoesNotWrite(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ctx = directoryInventorySupportContext(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	_, group := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")
	inventory := directoryInventoryFixture(t, ctx, ti, group, "builder")
	report, err := RunDirectoryRoleInventory(ctx, testenv.NewLogger(t), ti.conn, ti.service.authz, ti.service.audit, inventory, false, false)
	require.NoError(t, err)
	require.False(t, report.Committed)
	require.Equal(t, 1, report.AddedMappings)
	mappings, err := repo.New(ti.conn).ListDirectoryRoleMappings(ctx, orgID)
	require.NoError(t, err)
	require.Empty(t, mappings)
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestDirectoryRoleInventoryInvalidTargetsWriteNothing(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"missing role", "foreign role", "foreign group", "deleted group", "wrong WorkOS org", "wrong Gram org", "unvalidated session", "revoked operator"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAccessService(t)
			ctx = directoryInventorySupportContext(t, ctx, ti)
			orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
			seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
			_, group := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")
			inventory := directoryInventoryFixture(t, ctx, ti, group, "builder")
			switch scenario {
			case "missing role":
				inventory.Assignments = append(inventory.Assignments, DirectoryRoleInventoryAssignment{WorkOSGroupID: group, RoleSlug: "missing"})
			case "foreign role":
				seedOrganization(t, ctx, ti.conn, "other-org")
				seedRole(t, ctx, ti.conn, "other-org", mockRole("role_foreign", "Foreign", "foreign", ""))
				inventory.Assignments = append(inventory.Assignments, DirectoryRoleInventoryAssignment{WorkOSGroupID: group, RoleSlug: "foreign"})
			case "foreign group":
				seedOrganization(t, ctx, ti.conn, "other-org")
				_, foreign := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, "other-org", "Engineering")
				inventory.Assignments = append(inventory.Assignments, DirectoryRoleInventoryAssignment{WorkOSGroupID: foreign, RoleSlug: "builder"})
			case "deleted group":
				deleteMappingDirectoryGroup(t, ctx, ti.conn, group)
			case "wrong WorkOS org":
				inventory.WorkOSOrganizationID = "other-workos-org"
			case "wrong Gram org":
				inventory.OrganizationID = "other-org"
			case "unvalidated session":
				ctx = contextvalues.SetAuthContext(t.Context(), testAccessAuthContext(t, ctx))
			case "revoked operator":
				require.NoError(t, testrepo.New(ti.conn).SetUserPlatformAdminFixture(ctx, testrepo.SetUserPlatformAdminFixtureParams{ID: testAccessAuthContext(t, ctx).UserID, Admin: false}))
			}
			report, err := RunDirectoryRoleInventory(ctx, testenv.NewLogger(t), ti.conn, ti.service.authz, ti.service.audit, inventory, false, true)
			require.Error(t, err)
			require.False(t, report.Committed)
			mappings, err := repo.New(ti.conn).ListDirectoryRoleMappings(ctx, orgID)
			require.NoError(t, err)
			require.Empty(t, mappings)
			count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
			require.NoError(t, err)
			require.Zero(t, count)
		})
	}
}

func TestDirectoryRoleInventoryReportsStalePreservedMappings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ctx = directoryInventorySupportContext(t, ctx, ti)
	enableDirectoryRoleSetsForTest(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_viewer", "Viewer", "viewer", ""))
	groupID, group := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")
	viewer := seededRolePrincipal(t, ctx, ti.conn, orgID, "viewer").String()
	existing, err := ti.service.SetDirectoryRoleMappings(ctx, &gen.SetDirectoryRoleMappingsPayload{SourceKind: "group", DirectoryGroupID: new(groupID.String()), RoleUrns: []string{viewer}})
	require.NoError(t, err)
	_, err = repo.New(ti.conn).MarkOrganizationRoleDeletedLocally(ctx, repo.MarkOrganizationRoleDeletedLocallyParams{OrganizationID: orgID, WorkosSlug: "viewer"})
	require.NoError(t, err)
	baseline, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	inventory := directoryInventoryFixture(t, ctx, ti, group, "builder")
	report, err := RunDirectoryRoleInventory(ctx, testenv.NewLogger(t), ti.conn, ti.service.authz, ti.service.audit, inventory, false, true)
	require.Error(t, err)
	require.False(t, report.Committed)
	require.Equal(t, []DirectoryRoleInventoryStaleMapping{{WorkOSGroupID: group, RoleURN: viewer}}, report.StaleMappings)
	mappings, err := repo.New(ti.conn).ListDirectoryRoleMappings(ctx, orgID)
	require.NoError(t, err)
	require.Len(t, mappings, 1)
	require.Equal(t, existing[0].ID, mappings[0].ID.String())
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	require.Equal(t, baseline, after)
}

func TestDirectoryRoleInventoryRollsBackWriterFailure(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ctx = directoryInventorySupportContext(t, ctx, ti)
	installLegacyDirectoryMappingIndexesForTest(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_viewer", "Viewer", "viewer", ""))
	_, group := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")
	inventory := directoryInventoryFixture(t, ctx, ti, group, "builder")
	inventory.Assignments = append(inventory.Assignments, DirectoryRoleInventoryAssignment{WorkOSGroupID: group, RoleSlug: "viewer"})
	report, err := RunDirectoryRoleInventory(ctx, testenv.NewLogger(t), ti.conn, ti.service.authz, ti.service.audit, inventory, false, true)
	require.Error(t, err)
	require.False(t, report.Committed)
	mappings, err := repo.New(ti.conn).ListDirectoryRoleMappings(ctx, orgID)
	require.NoError(t, err)
	require.Empty(t, mappings)
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestDirectoryRoleInventoryShadowReportsLossAndRetainsOtherSources(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAccessService(t)
	ctx = directoryInventorySupportContext(t, ctx, ti)
	orgID := testAccessAuthContext(t, ctx).ActiveOrganizationID
	seedRole(t, ctx, ti.conn, orgID, mockRole("role_builder", "Builder", "builder", ""))
	builder := seededRolePrincipal(t, ctx, ti.conn, orgID, "builder").String()
	groupID, group := seedMappingDirectoryGroupWithWorkOSID(t, ctx, ti.conn, orgID, "Engineering")
	for _, userID := range []string{"mapped-member", "unmapped-member"} {
		seedConnectedUser(t, ctx, ti.conn, orgID, userID, userID+"@example.com", userID, "workos-"+userID, "membership-"+userID)
		seedRoleAssignment(t, ctx, ti.conn, orgID, userID, mockMember("", "membership-"+userID, "workos-"+userID, "builder"))
	}
	profile, workosUser := seedMappingDirectoryUser(t, ctx, ti.conn, orgID, "mapped-member", "mapped-member@example.com", `{}`)
	addMappingGroupMember(t, ctx, ti.conn, profile, workosUser, groupID, group)
	inventory := directoryInventoryFixture(t, ctx, ti, group, "builder")
	_, err := RunDirectoryRoleInventory(ctx, testenv.NewLogger(t), ti.conn, ti.service.authz, ti.service.audit, inventory, false, true)
	require.NoError(t, err)
	baseline, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	report, err := RunDirectoryRoleInventory(ctx, testenv.NewLogger(t), ti.conn, ti.service.authz, ti.service.audit, inventory, true, false)
	require.NoError(t, err)
	require.False(t, report.Committed)
	require.GreaterOrEqual(t, report.MembersChecked, 2)
	require.Equal(t, []DirectoryRoleInventoryDifference{{UserID: "unmapped-member", LostRoleURNs: []string{builder}, GainedRoleURNs: []string{}}}, report.Differences)
	principals, err := repo.New(ti.conn).ListUserRolePrincipals(ctx, repo.ListUserRolePrincipalsParams{OrganizationID: orgID, UserID: "unmapped-member"})
	require.NoError(t, err)
	require.Len(t, principals, 1)
	require.Equal(t, builder, principals[0].PrincipalUrn)
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDirectoryRoleMappingSet)
	require.NoError(t, err)
	require.Equal(t, baseline, after)
}

func TestDirectoryRoleInventoryDecode(t *testing.T) {
	t.Parallel()
	valid := `{"organization_id":"org-example","workos_organization_id":"workos-example","default_role_slug":"member","assignments":[]}`
	for _, tc := range []struct {
		name, input string
		valid       bool
	}{
		{"valid", valid, true}, {"unknown field", strings.Replace(valid, `"assignments"`, `"rules"`, 1), false},
		{"two objects", valid + valid, false}, {"incomplete", `{}`, false},
		{"oversized", valid + strings.Repeat(" ", maxDirectoryRoleInventoryBytes), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodeDirectoryRoleInventory(strings.NewReader(tc.input))
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestDirectoryRoleSupportCommandAuthentication(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"valid", "wrong organization", "expired", "ordinary session", "revoked staff", "unknown token"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestAccessService(t)
			ac := testAccessAuthContext(t, ctx)
			require.NoError(t, testrepo.New(ti.conn).SetUserPlatformAdminFixture(ctx, testrepo.SetUserPlatformAdminFixtureParams{ID: ac.UserID, Admin: scenario != "revoked staff"}))
			redisClient, err := infra.NewRedisClient(t, 0)
			require.NoError(t, err)
			logger := testenv.NewLogger(t)
			suffix := cache.Suffix(ac.ActiveOrganizationID)
			store := cache.NewTypedObjectCache[sessions.Session](logger, cache.NewRedisCacheAdapter(redisClient), suffix)
			now := time.Now()
			session := sessions.Session{SessionID: "support-token", UserID: ac.UserID, ActiveOrganizationID: ac.ActiveOrganizationID, SupportOrganizationID: ac.ActiveOrganizationID, SupportExpiresAt: now.Add(time.Hour)}
			if scenario == "ordinary session" {
				session.SupportOrganizationID = ""
			}
			if scenario == "expired" {
				now = session.SupportExpiresAt
			}
			require.NoError(t, store.Store(ctx, session))
			manager := sessions.NewManager(logger, testenv.NewTracerProvider(t), ti.conn, redisClient, suffix, nil, nil, nil)
			target, token := ac.ActiveOrganizationID, session.SessionID
			if scenario == "wrong organization" {
				target = "other-org"
			}
			if scenario == "unknown token" {
				token = "unknown-token"
			}
			authenticated, err := manager.AuthenticateSupportCommand(ctx, token, target, now)
			if scenario == "valid" {
				require.NoError(t, err)
				require.True(t, contextvalues.IsSupportSession(authenticated))
				resolved := testAccessAuthContext(t, authenticated)
				require.Equal(t, ac.UserID, resolved.UserID)
				require.Equal(t, ac.ActiveOrganizationID, resolved.ActiveOrganizationID)
			} else {
				require.Error(t, err)
			}
		})
	}
}
