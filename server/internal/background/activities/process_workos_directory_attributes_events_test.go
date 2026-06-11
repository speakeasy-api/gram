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
		{ID: "event_other_group", Event: "dsync.group.updated", CreatedAt: time.Now(), Data: directoryGroupEventData(workosOrgID, "directory_group_other", "Other")},
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
	require.JSONEq(t, string(directoryGroupEventData(workosOrgID, groupID, "Platform")), string(attributes))
	require.False(t, deleted)

	cursor, err := workosrepo.New(conn).GetOrganizationSyncLastEventID(ctx, workosOrgID)
	require.NoError(t, err)
	require.Equal(t, "event_group", cursor)
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
	})
	require.NoError(t, err)
	_, err = workosrepo.New(conn).UpsertDirectoryUser(ctx, workosrepo.UpsertDirectoryUserParams{
		OrganizationID:        gramOrgID,
		UserID:                conv.ToPGTextEmpty(""),
		WorkosDirectoryUserID: directoryUserID,
		Email:                 conv.ToPGText(email),
		Attributes:            []byte(`{"department":"Engineering","team":"SDK"}`),
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

	directoryUser, err := workosrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, directoryUserID)
	require.NoError(t, err)
	require.Equal(t, gramOrgID, directoryUser.OrganizationID)
	require.False(t, directoryUser.UserID.Valid)
	require.Equal(t, email, directoryUser.Email.String)
	require.JSONEq(t, string(directoryUserEventData(directoryUserID, email)), string(directoryUser.Attributes))

	_, _, attributes, deleted := getDirectoryGroupRow(t, ctx, conn, groupID)
	require.JSONEq(t, string(directoryGroupEventData(workosOrgID, groupID, "Platform")), string(attributes))
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
	})
	require.NoError(t, err)
	directoryUserIDUUID, err := workosrepo.New(conn).UpsertDirectoryUser(ctx, workosrepo.UpsertDirectoryUserParams{
		OrganizationID:        gramOrgID,
		UserID:                conv.ToPGTextEmpty(""),
		WorkosDirectoryUserID: directoryUserID,
		Email:                 conv.ToPGText("directory.group.delete@example.com"),
		Attributes:            []byte(`{}`),
	})
	require.NoError(t, err)
	_, err = workosrepo.New(conn).OpenDirectoryUserGroupMembership(ctx, workosrepo.OpenDirectoryUserGroupMembershipParams{
		DirectoryUserID:        directoryUserIDUUID,
		DirectoryGroupID:       groupIDUUID,
		WorkosDirectoryUserID:  directoryUserID,
		WorkosDirectoryGroupID: groupID,
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_group_deleted", Event: "dsync.group.deleted", CreatedAt: time.Now(), Data: directoryGroupEventData(workosOrgID, groupID, "Platform")},
	}})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_group_deleted", res.LastEventID)

	_, _, _, deleted := getDirectoryGroupRow(t, ctx, conn, groupID)
	require.True(t, deleted)
	require.Equal(t, 0, countCurrentMemberships(t, ctx, conn, groupID, directoryUserID))
}
