package directory_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/directory"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func namedInventory() fakeDirectoryInventory {
	listed := func(id, first, last string) workos.DirectoryUser {
		return workos.DirectoryUser{ID: id, DirectoryID: "directory_1", OrganizationID: "workos_org", FirstName: first, LastName: last}
	}
	return fakeDirectoryInventory{
		directories: []workos.Directory{{ID: "directory_1", OrganizationID: "workos_org"}},
		users: map[string][]workos.DirectoryUser{
			"directory_1": {
				listed("directory_user_unnamed", " Ada ", "Lovelace"),
				listed("directory_user_named", "Listed", "Name"),
				listed("directory_user_deleted", "Grace", "Hopper"),
				listed("directory_user_blank", "", ""),
			},
		},
	}
}

func TestFillDirectoryUserNamesOnlyFillsEmptyNames(t *testing.T) {
	t.Parallel()
	_, conn := newTestService(t)
	ctx := t.Context()
	const organizationID = "org_directory_names"
	seedOrganization(t, conn, organizationID)
	now := time.Now().UTC()
	seedDirectoryUser(t, conn, organizationID, "", "directory_user_unnamed", "unnamed@example.test", []byte(`{}`), now)
	seedDirectoryUser(t, conn, organizationID, "", "directory_user_blank", "blank@example.test", []byte(`{}`), now)
	seedDirectoryUser(t, conn, organizationID, "", "directory_user_deleted", "deleted@example.test", []byte(`{}`), now)
	// An event already reported a name: the listing must not overwrite it.
	_, err := directoryrepo.New(conn).UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        organizationID,
		WorkosDirectoryUserID: "directory_user_named",
		Email:                 conv.ToPGText("named@example.test"),
		FirstName:             conv.ToPGText("Event"),
		Attributes:            []byte(`{}`),
		RestoreDeleted:        true,
		WorkosCreatedAt:       conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(now),
		WorkosLastEventID:     conv.ToPGText("event_named"),
	})
	require.NoError(t, err)
	_, err = directoryrepo.New(conn).DeleteDirectoryUserByWorkOSID(ctx, directoryrepo.DeleteDirectoryUserByWorkOSIDParams{
		OrganizationID:  organizationID,
		WorkosDeletedAt: conv.ToPGTimestamptz(now.Add(time.Second)), WorkosLastEventID: conv.ToPGText("delete_event"), WorkosDirectoryUserID: "directory_user_deleted",
	})
	require.NoError(t, err)

	dryRun, err := directory.FillDirectoryUserNames(ctx, conn, namedInventory(), organizationID, "workos_org", true)
	require.NoError(t, err)
	require.Equal(t, 4, dryRun.ListedUsers)
	require.Equal(t, 3, dryRun.UsersWithName)
	// Only the live, unnamed row is filled: the named row keeps its event
	// name and the tombstoned row is skipped.
	require.Equal(t, int64(1), dryRun.UsersFilled)
	unnamed, err := directoryrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, "directory_user_unnamed")
	require.NoError(t, err)
	require.False(t, unnamed.FirstName.Valid, "dry-run must roll back")

	applied, err := directory.FillDirectoryUserNames(ctx, conn, namedInventory(), organizationID, "workos_org", false)
	require.NoError(t, err)
	require.Equal(t, int64(1), applied.UsersFilled)

	unnamed, err = directoryrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, "directory_user_unnamed")
	require.NoError(t, err)
	require.Equal(t, "Ada", unnamed.FirstName.String)
	require.Equal(t, "Lovelace", unnamed.LastName.String)
	require.Equal(t, "event_directory_user_unnamed", unnamed.WorkosLastEventID.String, "the event cursor is untouched")

	named, err := directoryrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, "directory_user_named")
	require.NoError(t, err)
	require.Equal(t, "Event", named.FirstName.String)
	require.False(t, named.LastName.Valid)

	blank, err := directoryrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, "directory_user_blank")
	require.NoError(t, err)
	require.False(t, blank.FirstName.Valid)
	require.False(t, blank.LastName.Valid)

	deleted, err := testrepo.New(conn).GetDirectoryUserLifecycleFixture(ctx, testrepo.GetDirectoryUserLifecycleFixtureParams{OrganizationID: organizationID, WorkosDirectoryUserID: "directory_user_deleted"})
	require.NoError(t, err)
	require.True(t, deleted.WorkosDeleted)
	require.Equal(t, "delete_event", deleted.WorkosLastEventID.String)

	repeated, err := directory.FillDirectoryUserNames(ctx, conn, namedInventory(), organizationID, "workos_org", false)
	require.NoError(t, err)
	require.Zero(t, repeated.UsersFilled)
}

func TestFillDirectoryUserNamesRejectsTenantMismatch(t *testing.T) {
	t.Parallel()
	_, conn := newTestService(t)
	const organizationID = "org_directory_names_mismatch"
	seedOrganization(t, conn, organizationID)
	inventory := namedInventory()
	inventory.users["directory_1"][0].OrganizationID = "another_org"
	_, err := directory.FillDirectoryUserNames(t.Context(), conn, inventory, organizationID, "workos_org", false)
	require.Error(t, err)
}
