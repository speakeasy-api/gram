package directory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/directory"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
)

type fakeDirectoryInventory struct {
	directories    []workos.Directory
	users          map[string][]workos.DirectoryUser
	groups         map[string][]workos.DirectoryGroup
	directoriesErr error
	usersErr       error
	groupsErr      error
	onGroups       func()
}

func (f fakeDirectoryInventory) ListDirectories(context.Context, string) ([]workos.Directory, error) {
	return f.directories, f.directoriesErr
}

func (f fakeDirectoryInventory) ListDirectoryUsers(_ context.Context, id string) ([]workos.DirectoryUser, error) {
	return f.users[id], f.usersErr
}

func (f fakeDirectoryInventory) ListDirectoryGroups(_ context.Context, id string) ([]workos.DirectoryGroup, error) {
	if f.onGroups != nil {
		f.onGroups()
	}
	return f.groups[id], f.groupsErr
}

func validInventory() fakeDirectoryInventory {
	return fakeDirectoryInventory{
		directories: []workos.Directory{{ID: "directory_1", OrganizationID: "workos_org"}},
		users: map[string][]workos.DirectoryUser{
			"directory_1": {{ID: "directory_user_match", DirectoryID: "directory_1", OrganizationID: "workos_org"}},
		},
		groups: map[string][]workos.DirectoryGroup{
			"directory_1": {{ID: "directory_group_match", DirectoryID: "directory_1", OrganizationID: "workos_org"}},
		},
	}
}

func TestAttributeDirectorySourcesRejectsChangedEventCursor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		before []string
		during []string
	}{
		{name: "cursor created", during: []string{"event_010"}},
		{name: "cursor advanced", before: []string{"event_001"}, during: []string{"event_010"}},
		{name: "duplicate replay", before: []string{"event_010"}, during: []string{"event_010"}},
		{name: "cursor returned to baseline", before: []string{"event_001"}, during: []string{"event_010", "event_001"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, conn := newTestService(t)
			ctx := t.Context()
			const organizationID = "org_attribution_cursor"
			seedOrganization(t, conn, organizationID)
			at := time.Now().UTC()
			seedDirectoryUser(t, conn, organizationID, "", "directory_user_match", "person@example.test", []byte(`{}`), at)
			seedDirectoryGroup(t, conn, organizationID, "directory_group_match", "Group", at)
			advance := func(ids []string) {
				for _, id := range ids {
					_, err := workosrepo.New(conn).SetOrganizationSyncLastEventID(ctx, workosrepo.SetOrganizationSyncLastEventIDParams{WorkosOrganizationID: "workos_org", LastEventID: id})
					require.NoError(t, err)
				}
			}
			advance(tc.before)
			inventory := validInventory()
			inventory.onGroups = func() { advance(tc.during) }
			_, err := directory.AttributeDirectorySources(ctx, conn, inventory, organizationID, "workos_org", false)
			require.ErrorContains(t, err, "organization events changed during inventory retrieval")
			fixtures := testrepo.New(conn)
			user, err := fixtures.GetDirectoryUserLifecycleFixture(ctx, testrepo.GetDirectoryUserLifecycleFixtureParams{OrganizationID: organizationID, WorkosDirectoryUserID: "directory_user_match"})
			require.NoError(t, err)
			require.False(t, user.DirectoryID.Valid)
			group, err := fixtures.GetDirectoryGroupLifecycleFixture(ctx, testrepo.GetDirectoryGroupLifecycleFixtureParams{OrganizationID: organizationID, WorkosDirectoryGroupID: "directory_group_match"})
			require.NoError(t, err)
			require.False(t, group.DirectoryID.Valid)
		})
	}
}

func TestAttributeDirectorySourcesDryRunAndRepeat(t *testing.T) {
	t.Parallel()
	_, conn := newTestService(t)
	ctx := t.Context()
	const organizationID = "org_directory_attribution"
	seedOrganization(t, conn, organizationID)
	now := time.Now().UTC()
	seedDirectoryUser(t, conn, organizationID, "", "directory_user_match", "private@example.test", []byte(`{"private":"attribute"}`), now)
	seedDirectoryGroup(t, conn, organizationID, "directory_group_match", "Private group", now)

	first, err := directory.AttributeDirectorySources(ctx, conn, validInventory(), organizationID, "workos_org", true)
	require.NoError(t, err)
	require.Equal(t, int64(1), first.UsersAttributed)
	require.Equal(t, int64(1), first.GroupsAttributed)
	require.Empty(t, first.Unmatched)
	user, err := directoryrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, "directory_user_match")
	require.NoError(t, err)
	require.False(t, user.DirectoryID.Valid, "dry-run must roll back the attribution")

	applied, err := directory.AttributeDirectorySources(ctx, conn, validInventory(), organizationID, "workos_org", false)
	require.NoError(t, err)
	require.Equal(t, int64(1), applied.UsersAttributed)
	require.Equal(t, int64(1), applied.GroupsAttributed)
	user, err = directoryrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, "directory_user_match")
	require.NoError(t, err)
	require.Equal(t, "directory_1", user.DirectoryID.String)

	repeated, err := directory.AttributeDirectorySources(ctx, conn, validInventory(), organizationID, "workos_org", false)
	require.NoError(t, err)
	require.Zero(t, repeated.UsersAttributed)
	require.Zero(t, repeated.GroupsAttributed)
}

func TestAttributeDirectorySourcesPreservesKnownAttributionAndTombstones(t *testing.T) {
	t.Parallel()
	_, conn := newTestService(t)
	ctx := t.Context()
	const organizationID = "org_directory_attribution_preserve"
	seedOrganization(t, conn, organizationID)
	now := time.Now().UTC()
	seedDirectoryUser(t, conn, organizationID, "", "directory_user_match", "deleted@example.test", []byte(`{}`), now)
	seedDirectoryGroup(t, conn, organizationID, "directory_group_match", "Group", now)
	_, err := directoryrepo.New(conn).DeleteDirectoryUserByWorkOSID(ctx, directoryrepo.DeleteDirectoryUserByWorkOSIDParams{
		OrganizationID:  organizationID,
		WorkosDeletedAt: conv.ToPGTimestamptz(now.Add(time.Second)), WorkosLastEventID: conv.ToPGText("delete_event"), WorkosDirectoryUserID: "directory_user_match",
	})
	require.NoError(t, err)
	_, err = directoryrepo.New(conn).AttributeDirectoryGroups(ctx, directoryrepo.AttributeDirectoryGroupsParams{OrganizationID: organizationID, WorkosDirectoryGroupIds: []string{"directory_group_match"}, DirectoryID: "known_directory"})
	require.NoError(t, err)
	report, err := directory.AttributeDirectorySources(ctx, conn, validInventory(), organizationID, "workos_org", false)
	require.NoError(t, err)
	require.Equal(t, int64(1), report.UsersAttributed)
	require.Zero(t, report.GroupsAttributed)
	fixtures := testrepo.New(conn)
	user, err := fixtures.GetDirectoryUserLifecycleFixture(ctx, testrepo.GetDirectoryUserLifecycleFixtureParams{OrganizationID: organizationID, WorkosDirectoryUserID: "directory_user_match"})
	require.NoError(t, err)
	require.Equal(t, "directory_1", user.DirectoryID.String)
	require.True(t, user.Deleted)
	require.True(t, user.WorkosDeleted)
	require.Equal(t, "delete_event", user.WorkosLastEventID.String)
	group, err := fixtures.GetDirectoryGroupLifecycleFixture(ctx, testrepo.GetDirectoryGroupLifecycleFixtureParams{OrganizationID: organizationID, WorkosDirectoryGroupID: "directory_group_match"})
	require.NoError(t, err)
	require.Equal(t, "known_directory", group.DirectoryID.String)
}

func TestAttributeDirectorySourcesLeavesUnknownAndTombstonedIDsUnmatched(t *testing.T) {
	t.Parallel()
	_, conn := newTestService(t)
	ctx := t.Context()
	const organizationID = "org_directory_attribution_residual"
	seedOrganization(t, conn, organizationID)
	now := time.Now().UTC()
	seedDirectoryUser(t, conn, organizationID, "", "directory_user_unknown", "unknown@example.test", []byte(`{}`), now)
	seedDirectoryUser(t, conn, organizationID, "", "directory_user_deleted", "deleted@example.test", []byte(`{}`), now)
	_, err := directoryrepo.New(conn).DeleteDirectoryUserByWorkOSID(ctx, directoryrepo.DeleteDirectoryUserByWorkOSIDParams{
		OrganizationID:  organizationID,
		WorkosDeletedAt: conv.ToPGTimestamptz(now.Add(time.Second)), WorkosLastEventID: conv.ToPGText("delete_event"), WorkosDirectoryUserID: "directory_user_deleted",
	})
	require.NoError(t, err)

	report, err := directory.AttributeDirectorySources(ctx, conn, validInventory(), organizationID, "workos_org", true)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"user:directory_user_deleted", "user:directory_user_unknown"}, report.Unmatched)
	require.Zero(t, report.UsersAttributed)
}

func TestAttributeDirectorySourcesRejectsTenantOrPayloadMismatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		inventory fakeDirectoryInventory
		workosOrg string
	}{
		{name: "requested organization mismatch", inventory: validInventory(), workosOrg: "another_org"},
		{name: "directory tenant mismatch", inventory: fakeDirectoryInventory{directories: []workos.Directory{{ID: "directory_1", OrganizationID: "another_org"}}}, workosOrg: "workos_org"},
		{name: "user parent mismatch", inventory: func() fakeDirectoryInventory {
			v := validInventory()
			v.users["directory_1"][0].DirectoryID = "other_directory"
			return v
		}(), workosOrg: "workos_org"},
		{name: "group tenant mismatch", inventory: func() fakeDirectoryInventory {
			v := validInventory()
			v.groups["directory_1"][0].OrganizationID = "another_org"
			return v
		}(), workosOrg: "workos_org"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, conn := newTestService(t)
			const organizationID = "org_directory_attribution_mismatch"
			seedOrganization(t, conn, organizationID)
			_, err := directory.AttributeDirectorySources(t.Context(), conn, tc.inventory, organizationID, tc.workosOrg, false)
			require.Error(t, err)
		})
	}
}

func TestAttributeDirectorySourcesStopsOnInventoryError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		set  func(*fakeDirectoryInventory, error)
	}{
		{name: "directories", set: func(inv *fakeDirectoryInventory, err error) { inv.directoriesErr = err }},
		{name: "users", set: func(inv *fakeDirectoryInventory, err error) { inv.usersErr = err }},
		{name: "groups", set: func(inv *fakeDirectoryInventory, err error) { inv.groupsErr = err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, conn := newTestService(t)
			inv := validInventory()
			tc.set(&inv, errors.New("inventory unavailable"))
			_, err := directory.AttributeDirectorySources(context.Background(), conn, inv, "org", "workos_org", false)
			require.ErrorContains(t, err, "inventory unavailable")
		})
	}
}
