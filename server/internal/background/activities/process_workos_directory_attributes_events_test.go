package activities_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
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

	err := conn.QueryRow(ctx, `
		SELECT organization_id, name, attributes, deleted
		FROM groups
		WHERE workos_directory_group_id = $1
	`, workosDirectoryGroupID).Scan(&organizationID, &name, &attributes, &deleted)
	require.NoError(t, err)
	return organizationID, name, attributes, deleted
}

func countCurrentMemberships(t *testing.T, ctx context.Context, conn workosrepo.DBTX, workosDirectoryGroupID, workosDirectoryUserID string) int {
	t.Helper()

	var count int
	err := conn.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM user_group_memberships
		WHERE workos_directory_group_id = $1
		  AND workos_directory_user_id = $2
	`, workosDirectoryGroupID, workosDirectoryUserID).Scan(&count)
	require.NoError(t, err)
	return count
}

func TestProcessWorkOSDirectoryAttributesEvents_UpsertsGroupAndAdvancesCursor(t *testing.T) {
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
	_, err := workosrepo.New(conn).SetDirectoryAttributesSyncLastEventID(ctx, workosrepo.SetDirectoryAttributesSyncLastEventIDParams{
		EntityType:  activities.WorkOSDirectoryAttributesEntityTypeGroup,
		EntityID:    groupID,
		LastEventID: "event_seed",
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_other_group", Event: "dsync.group.updated", CreatedAt: time.Now(), Data: directoryGroupEventData(workosOrgID, "directory_group_other", "Other")},
		{ID: "event_group", Event: "dsync.group.updated", CreatedAt: time.Now(), Data: directoryGroupEventData(workosOrgID, groupID, "Platform")},
	}})

	activity := activities.NewProcessWorkOSDirectoryAttributesEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, activities.ProcessWorkOSDirectoryAttributesEventsParams{
		EntityType: activities.WorkOSDirectoryAttributesEntityTypeGroup,
		EntityID:   groupID,
	})
	require.NoError(t, err)
	require.Equal(t, "event_seed", res.SinceEventID)
	require.Equal(t, "event_group", res.LastEventID)
	require.False(t, res.HasMore)

	calls := workosClient.EventCalls()
	require.Len(t, calls, 1)
	require.Equal(t, "event_seed", calls[0].After)

	organizationID, name, attributes, deleted := getDirectoryGroupRow(t, ctx, conn, groupID)
	require.Equal(t, gramOrgID, organizationID)
	require.Equal(t, "Platform", name)
	require.JSONEq(t, `{"id":"directory_group_upsert","organization_id":"org_directory_group","name":"Platform","raw_attributes":{"department":"Engineering"},"created_at":"2026-05-12T10:00:00Z","updated_at":"2026-05-12T10:00:00Z"}`, string(attributes))
	require.False(t, deleted)

	cursor, err := workosrepo.New(conn).GetDirectoryAttributesSyncLastEventID(ctx, workosrepo.GetDirectoryAttributesSyncLastEventIDParams{
		EntityType: activities.WorkOSDirectoryAttributesEntityTypeGroup,
		EntityID:   groupID,
	})
	require.NoError(t, err)
	require.Equal(t, "event_group", cursor)
}

func TestProcessWorkOSDirectoryAttributesEvents_OpensAndClosesMembership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_directory_attrs_membership")
	logger := testenv.NewLogger(t)

	const (
		gramOrgID       = "gram_directory_membership_org"
		workosOrgID     = "org_directory_membership"
		groupID         = "directory_group_membership"
		directoryUserID = "directory_user_membership"
		gramUserID      = "sb_directory_membership_user"
		email           = "directory.membership@example.com"
	)
	seedDirectoryAttributesWorkOSOrganization(t, ctx, conn, gramOrgID, workosOrgID)
	_, err := usersrepo.New(conn).UpsertUser(ctx, localUserParams(gramUserID, email, "Directory Member"))
	require.NoError(t, err)

	entityID := activities.DirectoryGroupMembershipSyncEntityID(groupID, directoryUserID)
	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_membership_added", Event: "dsync.group.user_added", CreatedAt: time.Now(), Data: directoryGroupMembershipEventData(workosOrgID, groupID, directoryUserID, email)},
	}})

	activity := activities.NewProcessWorkOSDirectoryAttributesEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, activities.ProcessWorkOSDirectoryAttributesEventsParams{
		EntityType: activities.WorkOSDirectoryAttributesEntityTypeGroupMembership,
		EntityID:   entityID,
	})
	require.NoError(t, err)
	require.Equal(t, "event_membership_added", res.LastEventID)
	require.Equal(t, 1, countCurrentMemberships(t, ctx, conn, groupID, directoryUserID))

	user, err := usersrepo.New(conn).GetUser(ctx, gramUserID)
	require.NoError(t, err)
	require.JSONEq(t, `{"id":"directory_user_membership","emails":[{"value":"directory.membership@example.com","primary":true}],"first_name":"Ada","last_name":"Lovelace","custom_attributes":{"department":"Engineering","team":"SDK"},"updated_at":"2026-05-12T10:00:00Z"}`, string(user.Attributes))

	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_membership_removed", Event: "dsync.group.user_removed", CreatedAt: time.Now(), Data: directoryGroupMembershipEventData(workosOrgID, groupID, directoryUserID, email)},
	}})
	res, err = activity.Do(ctx, activities.ProcessWorkOSDirectoryAttributesEventsParams{
		EntityType: activities.WorkOSDirectoryAttributesEntityTypeGroupMembership,
		EntityID:   entityID,
	})
	require.NoError(t, err)
	require.Equal(t, "event_membership_added", res.SinceEventID)
	require.Equal(t, "event_membership_removed", res.LastEventID)
	require.Equal(t, 0, countCurrentMemberships(t, ctx, conn, groupID, directoryUserID))
}

func TestProcessWorkOSDirectoryAttributesEvents_DeleteGroupClosesMemberships(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_directory_attrs_delete_group")
	logger := testenv.NewLogger(t)

	const (
		gramOrgID       = "gram_directory_group_delete_org"
		workosOrgID     = "org_directory_group_delete"
		groupID         = "directory_group_delete"
		directoryUserID = "directory_user_group_delete"
		gramUserID      = "sb_directory_group_delete_user"
	)
	seedDirectoryAttributesWorkOSOrganization(t, ctx, conn, gramOrgID, workosOrgID)
	_, err := usersrepo.New(conn).UpsertUser(ctx, localUserParams(gramUserID, "directory.group.delete@example.com", "Directory Delete"))
	require.NoError(t, err)

	groupUUID, err := workosrepo.New(conn).UpsertDirectoryGroup(ctx, workosrepo.UpsertDirectoryGroupParams{
		OrganizationID:         gramOrgID,
		WorkosDirectoryGroupID: groupID,
		Name:                   "Platform",
		Attributes:             []byte(`{"id":"directory_group_delete"}`),
		AttributesContentHash:  conv.ToPGText("sha256:seed"),
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, groupUUID)
	_, err = workosrepo.New(conn).OpenUserGroupMembership(ctx, workosrepo.OpenUserGroupMembershipParams{
		UserID:                 gramUserID,
		GroupID:                groupUUID,
		WorkosDirectoryUserID:  directoryUserID,
		WorkosDirectoryGroupID: groupID,
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_group_deleted", Event: "dsync.group.deleted", CreatedAt: time.Now(), Data: directoryGroupEventData(workosOrgID, groupID, "Platform")},
	}})

	activity := activities.NewProcessWorkOSDirectoryAttributesEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, activities.ProcessWorkOSDirectoryAttributesEventsParams{
		EntityType: activities.WorkOSDirectoryAttributesEntityTypeGroup,
		EntityID:   groupID,
	})
	require.NoError(t, err)
	require.Equal(t, "event_group_deleted", res.LastEventID)

	_, _, _, deleted := getDirectoryGroupRow(t, ctx, conn, groupID)
	require.True(t, deleted)
	require.Equal(t, 0, countCurrentMemberships(t, ctx, conn, groupID, directoryUserID))
}
