package activities_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
)

func directoryGroupEventData(workosOrgID, workosDirectoryGroupID, name string) []byte {
	return []byte(`{"id":"` + workosDirectoryGroupID + `","organization_id":"` + workosOrgID + `","name":"` + name + `","raw_attributes":{"department":"Engineering"},"created_at":"2026-05-12T10:00:00Z","updated_at":"2026-05-12T10:00:00Z"}`)
}

func directoryGroupEventDataWithUpdatedAt(workosOrgID, workosDirectoryGroupID, name, updatedAt string) []byte {
	return []byte(`{"id":"` + workosDirectoryGroupID + `","organization_id":"` + workosOrgID + `","name":"` + name + `","raw_attributes":{"department":"Engineering"},"created_at":"2026-05-12T10:00:00Z","updated_at":"` + updatedAt + `"}`)
}

func directoryGroupEventDataWithoutRawAttributes(workosOrgID, workosDirectoryGroupID, name string) []byte {
	return []byte(`{"id":"` + workosDirectoryGroupID + `","organization_id":"` + workosOrgID + `","name":"` + name + `","created_at":"2026-05-12T10:00:00Z","updated_at":"2026-05-12T10:00:00Z"}`)
}

func directoryGroupMembershipEventData(workosOrgID, workosDirectoryGroupID, workosDirectoryUserID, email string) []byte {
	return []byte(`{"group":` + string(directoryGroupEventData(workosOrgID, workosDirectoryGroupID, "Platform")) + `,"user":` + string(directoryUserEventData(workosDirectoryUserID, email)) + `}`)
}

func seedDirectoryAttributesWorkOSOrganization(t *testing.T, ctx context.Context, conn orgrepo.DBTX, gramOrgID, workosOrgID string) {
	t.Helper()

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:       gramOrgID,
		Name:     gramOrgID,
		Slug:     gramOrgID,
		WorkosID: conv.ToPGText(workosOrgID),
	})
	require.NoError(t, err)
}

func getDirectoryGroupRow(t *testing.T, ctx context.Context, conn workosrepo.DBTX, workosDirectoryGroupID string) (organizationID string, name string, attributes []byte, deleted bool) {
	t.Helper()

	row, err := workosrepo.New(conn).GetDirectoryGroupByWorkOSID(ctx, workosDirectoryGroupID)
	require.NoError(t, err)
	return row.OrganizationID, row.Name, row.Attributes, row.Deleted
}

func countCurrentMemberships(t *testing.T, ctx context.Context, conn workosrepo.DBTX, workosDirectoryGroupID, workosDirectoryUserID string) int {
	t.Helper()

	count, err := workosrepo.New(conn).CountDirectoryUserGroupMembershipsByWorkOSIDs(ctx, workosrepo.CountDirectoryUserGroupMembershipsByWorkOSIDsParams{
		WorkosDirectoryGroupID: workosDirectoryGroupID,
		WorkosDirectoryUserID:  workosDirectoryUserID,
	})
	require.NoError(t, err)
	return int(count)
}

func directorySyncTime() time.Time {
	return time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
}

func TestProcessWorkOSOrganizationEvents_UpsertsDirectoryGroupAndAdvancesOrganizationCursor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_directory_attrs_group")
	logger := testenv.NewLogger(t)

	const (
		gramOrgID   = "gram_directory_group_org"
		workosOrgID = "org_directory_group"
		groupID     = "directory_group_upsert"
	)
	seedDirectoryAttributesWorkOSOrganization(t, ctx, conn, gramOrgID, workosOrgID)
	_, err := workosrepo.New(conn).SetOrganizationSyncLastEventID(ctx, workosrepo.SetOrganizationSyncLastEventIDParams{
		WorkosOrganizationID: workosOrgID,
		LastEventID:          "event_seed",
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_other_group", Event: "dsync.group.updated", CreatedAt: time.Now(), Data: directoryGroupEventDataWithoutRawAttributes(workosOrgID, "directory_group_other", "Other")},
		{ID: "event_group", Event: "dsync.group.updated", CreatedAt: time.Now(), Data: directoryGroupEventData(workosOrgID, groupID, "Platform")},
	}})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_seed", res.SinceEventID)
	require.Equal(t, "event_group", res.LastEventID)
	require.False(t, res.HasMore)

	calls := workosClient.EventCalls()
	require.Len(t, calls, 1)
	require.Equal(t, "event_seed", calls[0].After)
	require.Equal(t, workosOrgID, calls[0].OrganizationId)

	organizationID, name, attributes, deleted := getDirectoryGroupRow(t, ctx, conn, groupID)
	require.Equal(t, gramOrgID, organizationID)
	require.Equal(t, "Platform", name)
	require.JSONEq(t, `{"department":"Engineering"}`, string(attributes))
	require.False(t, deleted)

	// Group events without raw_attributes store an empty object.
	_, _, otherAttributes, _ := getDirectoryGroupRow(t, ctx, conn, "directory_group_other")
	require.JSONEq(t, `{}`, string(otherAttributes))

	cursor, err := workosrepo.New(conn).GetOrganizationSyncLastEventID(ctx, workosOrgID)
	require.NoError(t, err)
	require.Equal(t, "event_group", cursor)
}

func TestProcessWorkOSOrganizationEvents_SkipsStaleDirectoryGroupEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_directory_attrs_stale_group")
	logger := testenv.NewLogger(t)

	const (
		gramOrgID   = "gram_directory_group_stale_org"
		workosOrgID = "org_directory_group_stale"
		groupID     = "directory_group_stale"
	)
	seedDirectoryAttributesWorkOSOrganization(t, ctx, conn, gramOrgID, workosOrgID)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_0002", Event: "dsync.group.updated", CreatedAt: directorySyncTime().Add(time.Hour), Data: directoryGroupEventDataWithUpdatedAt(workosOrgID, groupID, "New Name", "2026-05-12T11:00:00Z")},
		{ID: "event_0001", Event: "dsync.group.updated", CreatedAt: directorySyncTime(), Data: directoryGroupEventDataWithUpdatedAt(workosOrgID, groupID, "Old Name", "2026-05-12T10:00:00Z")},
	}})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_0001", res.LastEventID)

	_, name, _, deleted := getDirectoryGroupRow(t, ctx, conn, groupID)
	require.Equal(t, "New Name", name)
	require.False(t, deleted)
}

func TestProcessWorkOSOrganizationEvents_OpensAndClosesDirectoryMembership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_directory_attrs_membership")
	logger := testenv.NewLogger(t)

	const (
		gramOrgID       = "gram_directory_membership_org"
		workosOrgID     = "org_directory_membership"
		groupID         = "directory_group_membership"
		directoryUserID = "directory_user_membership"
		email           = "directory.membership@example.com"
	)
	seedDirectoryAttributesWorkOSOrganization(t, ctx, conn, gramOrgID, workosOrgID)
	_, err := workosrepo.New(conn).UpsertDirectoryGroup(ctx, workosrepo.UpsertDirectoryGroupParams{
		OrganizationID:         gramOrgID,
		WorkosDirectoryGroupID: groupID,
		Name:                   "Platform",
		Attributes:             []byte(`{"id":"directory_group_membership","source":"group-event"}`),
		WorkosCreatedAt:        conv.ToPGTimestamptz(directorySyncTime()),
		WorkosUpdatedAt:        conv.ToPGTimestamptz(directorySyncTime()),
		WorkosLastEventID:      conv.ToPGText("event_seed_group"),
	})
	require.NoError(t, err)
	_, err = workosrepo.New(conn).UpsertDirectoryUser(ctx, workosrepo.UpsertDirectoryUserParams{
		OrganizationID:        gramOrgID,
		UserID:                conv.ToPGTextEmpty(""),
		WorkosDirectoryUserID: directoryUserID,
		Email:                 conv.ToPGText(email),
		Attributes:            []byte(`{"department":"Engineering","team":"SDK"}`),
		WorkosCreatedAt:       conv.ToPGTimestamptz(directorySyncTime()),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(directorySyncTime()),
		WorkosLastEventID:     conv.ToPGText("event_seed_user"),
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_membership_added", Event: "dsync.group.user_added", CreatedAt: time.Now(), Data: directoryGroupMembershipEventData(workosOrgID, groupID, directoryUserID, email)},
	}})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_membership_added", res.LastEventID)
	require.Equal(t, 1, countCurrentMemberships(t, ctx, conn, groupID, directoryUserID))

	// Membership events do not upsert the embedded user/group payloads;
	// created/updated events are the source of entity state.
	directoryUser, err := workosrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, directoryUserID)
	require.NoError(t, err)
	require.Equal(t, gramOrgID, directoryUser.OrganizationID)
	require.False(t, directoryUser.UserID.Valid)
	require.Equal(t, email, directoryUser.Email.String)
	require.JSONEq(t, `{"department":"Engineering","team":"SDK"}`, string(directoryUser.Attributes))

	_, _, attributes, deleted := getDirectoryGroupRow(t, ctx, conn, groupID)
	require.JSONEq(t, `{"id":"directory_group_membership","source":"group-event"}`, string(attributes))
	require.False(t, deleted)

	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_membership_removed", Event: "dsync.group.user_removed", CreatedAt: time.Now(), Data: directoryGroupMembershipEventData(workosOrgID, groupID, directoryUserID, email)},
	}})
	res, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_membership_added", res.SinceEventID)
	require.Equal(t, "event_membership_removed", res.LastEventID)
	require.Equal(t, 0, countCurrentMemberships(t, ctx, conn, groupID, directoryUserID))
}

func TestProcessWorkOSOrganizationEvents_RemoveMissingDirectoryMembershipNoops(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_directory_attrs_missing_membership")
	logger := testenv.NewLogger(t)

	const (
		gramOrgID       = "gram_directory_membership_missing_org"
		workosOrgID     = "org_directory_membership_missing"
		groupID         = "directory_group_membership_missing"
		directoryUserID = "directory_user_membership_missing"
		email           = "directory.membership.missing@example.com"
	)
	seedDirectoryAttributesWorkOSOrganization(t, ctx, conn, gramOrgID, workosOrgID)
	_, err := workosrepo.New(conn).UpsertDirectoryGroup(ctx, workosrepo.UpsertDirectoryGroupParams{
		OrganizationID:         gramOrgID,
		WorkosDirectoryGroupID: groupID,
		Name:                   "Platform",
		Attributes:             []byte(`{}`),
		WorkosCreatedAt:        conv.ToPGTimestamptz(directorySyncTime()),
		WorkosUpdatedAt:        conv.ToPGTimestamptz(directorySyncTime()),
		WorkosLastEventID:      conv.ToPGText("event_seed_group"),
	})
	require.NoError(t, err)
	_, err = workosrepo.New(conn).UpsertDirectoryUser(ctx, workosrepo.UpsertDirectoryUserParams{
		OrganizationID:        gramOrgID,
		UserID:                conv.ToPGTextEmpty(""),
		WorkosDirectoryUserID: directoryUserID,
		Email:                 conv.ToPGText(email),
		Attributes:            []byte(`{}`),
		WorkosCreatedAt:       conv.ToPGTimestamptz(directorySyncTime()),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(directorySyncTime()),
		WorkosLastEventID:     conv.ToPGText("event_seed_user"),
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_missing_membership_removed", Event: "dsync.group.user_removed", CreatedAt: directorySyncTime(), Data: directoryGroupMembershipEventData(workosOrgID, groupID, directoryUserID, email)},
	}})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_missing_membership_removed", res.LastEventID)
	require.Equal(t, 0, countCurrentMemberships(t, ctx, conn, groupID, directoryUserID))
}

func TestProcessWorkOSOrganizationEvents_DeleteDirectoryGroupClosesMemberships(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_directory_attrs_delete_group")
	logger := testenv.NewLogger(t)

	const (
		gramOrgID       = "gram_directory_group_delete_org"
		workosOrgID     = "org_directory_group_delete"
		groupID         = "directory_group_delete"
		directoryUserID = "directory_user_group_delete"
	)
	seedDirectoryAttributesWorkOSOrganization(t, ctx, conn, gramOrgID, workosOrgID)

	groupIDUUID, err := workosrepo.New(conn).UpsertDirectoryGroup(ctx, workosrepo.UpsertDirectoryGroupParams{
		OrganizationID:         gramOrgID,
		WorkosDirectoryGroupID: groupID,
		Name:                   "Platform",
		Attributes:             []byte(`{"id":"directory_group_delete"}`),
		WorkosCreatedAt:        conv.ToPGTimestamptz(directorySyncTime()),
		WorkosUpdatedAt:        conv.ToPGTimestamptz(directorySyncTime()),
		WorkosLastEventID:      conv.ToPGText("event_0000"),
	})
	require.NoError(t, err)
	directoryUserIDUUID, err := workosrepo.New(conn).UpsertDirectoryUser(ctx, workosrepo.UpsertDirectoryUserParams{
		OrganizationID:        gramOrgID,
		UserID:                conv.ToPGTextEmpty(""),
		WorkosDirectoryUserID: directoryUserID,
		Email:                 conv.ToPGText("directory.group.delete@example.com"),
		Attributes:            []byte(`{}`),
		WorkosCreatedAt:       conv.ToPGTimestamptz(directorySyncTime()),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(directorySyncTime()),
		WorkosLastEventID:     conv.ToPGText("event_seed_user"),
	})
	require.NoError(t, err)
	_, err = workosrepo.New(conn).OpenDirectoryUserGroupMembership(ctx, workosrepo.OpenDirectoryUserGroupMembershipParams{
		DirectoryUserID:        directoryUserIDUUID,
		DirectoryGroupID:       groupIDUUID,
		WorkosDirectoryUserID:  directoryUserID,
		WorkosDirectoryGroupID: groupID,
		WorkosCreatedAt:        conv.ToPGTimestamptz(directorySyncTime()),
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_0001", Event: "dsync.group.deleted", CreatedAt: time.Now(), Data: directoryGroupEventData(workosOrgID, groupID, "Platform")},
	}})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_0001", res.LastEventID)

	_, _, _, deleted := getDirectoryGroupRow(t, ctx, conn, groupID)
	require.True(t, deleted)
	require.Equal(t, 0, countCurrentMemberships(t, ctx, conn, groupID, directoryUserID))
}
