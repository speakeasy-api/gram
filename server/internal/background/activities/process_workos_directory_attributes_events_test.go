package activities_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	directoryrepo "github.com/speakeasy-api/gram/server/internal/directory/repo"
	"github.com/speakeasy-api/gram/server/internal/directoryreview"
	"github.com/speakeasy-api/gram/server/internal/feature"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/signins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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

func getDirectoryGroupRow(t *testing.T, ctx context.Context, conn directoryrepo.DBTX, workosDirectoryGroupID string) (organizationID string, name string, attributes []byte, deleted bool) {
	t.Helper()

	row, err := directoryrepo.New(conn).GetDirectoryGroupByWorkOSID(ctx, workosDirectoryGroupID)
	require.NoError(t, err)
	return row.OrganizationID, row.Name, row.Attributes, row.Deleted
}

func countCurrentMemberships(t *testing.T, ctx context.Context, conn directoryrepo.DBTX, workosDirectoryGroupID, workosDirectoryUserID string) int {
	t.Helper()

	count, err := directoryrepo.New(conn).CountDirectoryUserGroupMembershipsByWorkOSIDs(ctx, directoryrepo.CountDirectoryUserGroupMembershipsByWorkOSIDsParams{
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

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache, nil)
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

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache, nil)
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
	_, err := directoryrepo.New(conn).UpsertDirectoryGroup(ctx, directoryrepo.UpsertDirectoryGroupParams{
		OrganizationID:         gramOrgID,
		WorkosDirectoryGroupID: groupID,
		Name:                   "Platform",
		Attributes:             []byte(`{"id":"directory_group_membership","source":"group-event"}`),
		WorkosCreatedAt:        conv.ToPGTimestamptz(directorySyncTime()),
		WorkosUpdatedAt:        conv.ToPGTimestamptz(directorySyncTime()),
		WorkosLastEventID:      conv.ToPGText("event_seed_group"),
	})
	require.NoError(t, err)
	_, err = directoryrepo.New(conn).UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        gramOrgID,
		UserID:                conv.ToPGTextEmpty(""),
		WorkosDirectoryUserID: directoryUserID,
		Email:                 conv.ToPGText(email),
		Attributes:            []byte(`{"department":"Engineering","team":"SDK"}`),
		RestoreDeleted:        true,
		WorkosCreatedAt:       conv.ToPGTimestamptz(directorySyncTime()),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(directorySyncTime()),
		WorkosLastEventID:     conv.ToPGText("event_seed_user"),
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_membership_added", Event: "dsync.group.user_added", CreatedAt: time.Now(), Data: directoryGroupMembershipEventData(workosOrgID, groupID, directoryUserID, email)},
	}})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache, nil)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_membership_added", res.LastEventID)
	require.Equal(t, 1, countCurrentMemberships(t, ctx, conn, groupID, directoryUserID))

	// Membership events do not upsert the embedded user/group payloads;
	// created/updated events are the source of entity state.
	directoryUser, err := directoryrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, directoryUserID)
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

func TestProcessWorkOSOrganizationEvents_MembershipForUnknownGroupCreatesGroupAndOpensMembership(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "workos_directory_attrs_unknown_group")
	logger := testenv.NewLogger(t)

	const (
		gramOrgID       = "gram_directory_unknown_group_org"
		workosOrgID     = "org_directory_unknown_group"
		groupID         = "directory_group_unknown"
		directoryUserID = "directory_user_unknown_group"
		email           = "directory.unknown.group@example.com"
	)
	seedDirectoryAttributesWorkOSOrganization(t, ctx, conn, gramOrgID, workosOrgID)
	_, err := directoryrepo.New(conn).UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        gramOrgID,
		UserID:                conv.ToPGTextEmpty(""),
		WorkosDirectoryUserID: directoryUserID,
		Email:                 conv.ToPGText(email),
		Attributes:            []byte(`{}`),
		RestoreDeleted:        true,
		WorkosCreatedAt:       conv.ToPGTimestamptz(directorySyncTime()),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(directorySyncTime()),
		WorkosLastEventID:     conv.ToPGText("event_seed_user"),
	})
	require.NoError(t, err)

	// The cursor already sits past the group's history: its
	// dsync.group.created event and earlier membership events were never
	// fetched, so this later membership event is the first time the group is
	// seen.
	_, err = workosrepo.New(conn).SetOrganizationSyncLastEventID(ctx, workosrepo.SetOrganizationSyncLastEventIDParams{
		WorkosOrganizationID: workosOrgID,
		LastEventID:          "event_cursor_past_group_history",
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_unknown_group_user_added", Event: "dsync.group.user_added", CreatedAt: time.Now(), Data: directoryGroupMembershipEventData(workosOrgID, groupID, directoryUserID, email)},
	}})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache, nil)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_cursor_past_group_history", workosClient.EventCalls()[0].After, "only events after the cursor are fetched")
	require.Equal(t, "event_unknown_group_user_added", res.LastEventID)
	require.Equal(t, 1, countCurrentMemberships(t, ctx, conn, groupID, directoryUserID))

	organizationID, name, _, deleted := getDirectoryGroupRow(t, ctx, conn, groupID)
	require.Equal(t, gramOrgID, organizationID)
	require.Equal(t, "Platform", name)
	require.False(t, deleted)
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
	_, err := directoryrepo.New(conn).UpsertDirectoryGroup(ctx, directoryrepo.UpsertDirectoryGroupParams{
		OrganizationID:         gramOrgID,
		WorkosDirectoryGroupID: groupID,
		Name:                   "Platform",
		Attributes:             []byte(`{}`),
		WorkosCreatedAt:        conv.ToPGTimestamptz(directorySyncTime()),
		WorkosUpdatedAt:        conv.ToPGTimestamptz(directorySyncTime()),
		WorkosLastEventID:      conv.ToPGText("event_seed_group"),
	})
	require.NoError(t, err)
	_, err = directoryrepo.New(conn).UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        gramOrgID,
		UserID:                conv.ToPGTextEmpty(""),
		WorkosDirectoryUserID: directoryUserID,
		Email:                 conv.ToPGText(email),
		Attributes:            []byte(`{}`),
		RestoreDeleted:        true,
		WorkosCreatedAt:       conv.ToPGTimestamptz(directorySyncTime()),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(directorySyncTime()),
		WorkosLastEventID:     conv.ToPGText("event_seed_user"),
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_missing_membership_removed", Event: "dsync.group.user_removed", CreatedAt: directorySyncTime(), Data: directoryGroupMembershipEventData(workosOrgID, groupID, directoryUserID, email)},
	}})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache, nil)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_missing_membership_removed", res.LastEventID)
	require.Equal(t, 0, countCurrentMemberships(t, ctx, conn, groupID, directoryUserID))
}

func TestProcessWorkOSOrganizationEvents_DeleteDirectoryGroupClosesAndPreventsReopeningMemberships(t *testing.T) {
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

	groupIDUUID, err := directoryrepo.New(conn).UpsertDirectoryGroup(ctx, directoryrepo.UpsertDirectoryGroupParams{
		OrganizationID:         gramOrgID,
		WorkosDirectoryGroupID: groupID,
		Name:                   "Platform",
		Attributes:             []byte(`{"id":"directory_group_delete"}`),
		WorkosCreatedAt:        conv.ToPGTimestamptz(directorySyncTime()),
		WorkosUpdatedAt:        conv.ToPGTimestamptz(directorySyncTime()),
		WorkosLastEventID:      conv.ToPGText("event_0000"),
	})
	require.NoError(t, err)
	directoryUserIDUUID, err := directoryrepo.New(conn).UpsertDirectoryUser(ctx, directoryrepo.UpsertDirectoryUserParams{
		OrganizationID:        gramOrgID,
		UserID:                conv.ToPGTextEmpty(""),
		WorkosDirectoryUserID: directoryUserID,
		Email:                 conv.ToPGText("directory.group.delete@example.com"),
		Attributes:            []byte(`{}`),
		RestoreDeleted:        true,
		WorkosCreatedAt:       conv.ToPGTimestamptz(directorySyncTime()),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(directorySyncTime()),
		WorkosLastEventID:     conv.ToPGText("event_seed_user"),
	})
	require.NoError(t, err)
	_, err = directoryrepo.New(conn).OpenDirectoryUserGroupMembership(ctx, directoryrepo.OpenDirectoryUserGroupMembershipParams{
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

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache, nil)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_0001", res.LastEventID)

	_, _, _, deleted := getDirectoryGroupRow(t, ctx, conn, groupID)
	require.True(t, deleted)
	require.Equal(t, 0, countCurrentMemberships(t, ctx, conn, groupID, directoryUserID))

	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_0002", Event: "dsync.group.user_added", CreatedAt: time.Now(), Data: directoryGroupMembershipEventData(workosOrgID, groupID, directoryUserID, "directory.group.delete@example.com")},
	}})
	res, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_0001", res.SinceEventID)
	require.Equal(t, "event_0002", res.LastEventID)
	require.Equal(t, 0, countCurrentMemberships(t, ctx, conn, groupID, directoryUserID))
}

func directoryUserEventDataWithState(workosOrgID, workosDirectoryUserID, email, state, updatedAt string) []byte {
	return []byte(`{"id":"` + workosDirectoryUserID + `","organization_id":"` + workosOrgID + `","email":"` + email + `","first_name":"Ada","last_name":"Lovelace","state":"` + state + `","custom_attributes":{"department":"Engineering"},"created_at":"2026-05-12T10:00:00Z","updated_at":"` + updatedAt + `"}`)
}

func directoryUserEventDataWithoutState(workosOrgID, workosDirectoryUserID, email, updatedAt string) []byte {
	return []byte(`{"id":"` + workosDirectoryUserID + `","organization_id":"` + workosOrgID + `","email":"` + email + `","first_name":"Ada","last_name":"Lovelace","custom_attributes":{"department":"Engineering"},"created_at":"2026-05-12T10:00:00Z","updated_at":"` + updatedAt + `"}`)
}

func TestProcessWorkOSOrganizationEvents_DirectoryUserEventsStoreNames(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_directory_user_names")
	logger := testenv.NewLogger(t)

	const (
		gramOrgID   = "gram_directory_user_names_org"
		workosOrgID = "org_directory_user_names"
		userID      = "directory_user_names"
	)
	seedDirectoryAttributesWorkOSOrganization(t, ctx, conn, gramOrgID, workosOrgID)

	unnamed := []byte(`{"id":"` + userID + `","organization_id":"` + workosOrgID + `","email":"names@example.com","first_name":"","state":"active","custom_attributes":{},"created_at":"2026-05-12T10:00:00Z","updated_at":"2026-05-12T10:02:00Z"}`)
	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_named", Event: "dsync.user.created", CreatedAt: time.Now(), Data: directoryUserEventDataWithState(workosOrgID, userID, "names@example.com", "active", "2026-05-12T10:01:00Z")},
	}, {
		{ID: "event_unnamed", Event: "dsync.user.updated", CreatedAt: time.Now(), Data: unnamed},
	}})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache, nil)
	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := directoryrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, "Ada", row.FirstName.String)
	require.Equal(t, "Lovelace", row.LastName.String)

	// The newest event wins, including when the directory stops reporting a name.
	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	row, err = directoryrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, userID)
	require.NoError(t, err)
	require.False(t, row.FirstName.Valid)
	require.False(t, row.LastName.Valid)
}

func TestProcessWorkOSOrganizationEvents_DirectoryUserDeactivationDeprovisionsAccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_directory_user_deactivate")
	logger := testenv.NewLogger(t)

	const (
		organizationID  = "gram_org_dsync_deactivate"
		workosOrgID     = "org_01HZDSYNCDEACT"
		userID          = "user_dsync_deactivate"
		workosUserID    = "user_01HZDSYNCDEACT"
		membershipID    = "mem_01HZDSYNCDEACT"
		directoryUserID = "directory_user_deactivate"
	)
	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedWorkOSUser(t, ctx, conn, userID, workosUserID)
	seedOrganizationRole(t, ctx, conn, organizationID, "member")
	_, err := orgrepo.New(conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID, UserID: conv.ToPGText(userID),
	})
	require.NoError(t, err)
	agent, err := agentrepo.New(conn).CreateAgent(ctx, agentrepo.CreateAgentParams{
		OrganizationID: organizationID, OwnerUserID: userID, Name: "Deactivated directory owner agent",
	})
	require.NoError(t, err)

	// The seeded user's email matches the directory user payload so the
	// deactivation can resolve the Speakeasy user by email.
	email := userID + "@example.com"

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		newWorkOSMembershipEvent(t, "organization_membership.created", "event_0001", membershipID, workosOrgID, workosUserID, time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC), "member"),
		{ID: "event_0002", Event: "dsync.user.created", CreatedAt: time.Date(2026, 5, 12, 12, 30, 0, 0, time.UTC), Data: directoryUserEventDataWithState(workosOrgID, directoryUserID, email, "active", "2026-05-12T12:30:00Z")},
		// SCIM deactivation: the IdP suspends the user and WorkOS emits a
		// dsync.user.updated event with state=inactive.
		{ID: "event_0003", Event: "dsync.user.updated", CreatedAt: time.Date(2026, 5, 12, 13, 0, 0, 0, time.UTC), Data: directoryUserEventDataWithState(workosOrgID, directoryUserID, email, "inactive", "2026-05-12T13:00:00Z")},
	}})

	capturingCache := newCaptureCache()
	signals := &recordingIdentityMapSignaler{mu: sync.Mutex{}, count: 0}
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, capturingCache, signals)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_0003", res.LastEventID)

	// Both the membership upsert and the deactivation's deprovision request an
	// identity map refresh; the active directory-user upsert does not.
	require.Equal(t, 2, signals.refreshCount())

	// The directory user row is soft-deleted.
	_, err = directoryrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, directoryUserID)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// The user's organization access is deprovisioned.
	relationship, err := orgrepo.New(conn).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
	require.True(t, relationship.Deleted)
	require.Equal(t, "event_0003", relationship.WorkosLastEventID.String)

	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.True(t, assignments[0].DeletedAt.Valid)
	latched, err := agentrepo.New(conn).GetAgentByID(ctx, agentrepo.GetAgentByIDParams{OrganizationID: organizationID, ID: agent.ID})
	require.NoError(t, err)
	require.True(t, latched.OwnerReassignmentRequiredAt.Valid)
	require.Equal(t, "owner_inactive", latched.OwnerReassignmentReason.String)

	// Cached user info is invalidated on the membership add and again on the
	// deprovisioning, so org-access checks never wait out the cache TTL.
	deletedKeys := capturingCache.Deleted()
	require.Len(t, deletedKeys, 2)
	for _, key := range deletedKeys {
		require.Contains(t, key, sessions.UserInfoCacheKey(userID))
	}
}

func TestProcessWorkOSOrganizationEvents_DirectoryUserDeactivationRevokesSignIns(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"on", "off", "undecided"} {
		t.Run("flag "+flag, func(t *testing.T) {
			t.Parallel()
			deactivateDirectoryUserWithSignIn(t, flag)
		})
	}
}

// flag "undecided" leaves the flag unset, as during a flag-service outage:
// deprovisioning must still revoke.
func deactivateDirectoryUserWithSignIn(t *testing.T, flag string) {
	t.Helper()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_directory_user_deactivate_sign_ins_"+flag)
	logger := testenv.NewLogger(t)

	const (
		organizationID  = "gram_org_dsync_sign_ins"
		workosOrgID     = "org_01HZDSYNCSIGNINS"
		userID          = "user_dsync_sign_ins"
		workosUserID    = "user_01HZDSYNCSIGNINS"
		membershipID    = "mem_01HZDSYNCSIGNINS"
		directoryUserID = "directory_user_sign_ins"
		workosSessionID = "session_dsync_sign_ins"
	)
	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedWorkOSUser(t, ctx, conn, userID, workosUserID)
	seedOrganizationRole(t, ctx, conn, organizationID, "member")
	_, err := orgrepo.New(conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID, UserID: conv.ToPGText(userID),
	})
	require.NoError(t, err)
	signIn, err := signins.Record(ctx, conn, signins.RecordParams{
		OrganizationID: organizationID, UserID: userID, WorkOSSessionID: workosSessionID,
		AuthenticationMethod: signins.MethodSSO, AuthenticatedAt: time.Now(),
	})
	require.NoError(t, err)
	require.True(t, signIn.ID.Valid)

	email := userID + "@example.com"
	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		newWorkOSMembershipEvent(t, "organization_membership.created", "event_0001", membershipID, workosOrgID, workosUserID, time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC), "member"),
		{ID: "event_0002", Event: "dsync.user.created", CreatedAt: time.Date(2026, 5, 12, 12, 30, 0, 0, time.UTC), Data: directoryUserEventDataWithState(workosOrgID, directoryUserID, email, "active", "2026-05-12T12:30:00Z")},
		{ID: "event_0003", Event: "dsync.user.updated", CreatedAt: time.Date(2026, 5, 12, 13, 0, 0, 0, time.UTC), Data: directoryUserEventDataWithState(workosOrgID, directoryUserID, email, "inactive", "2026-05-12T13:00:00Z")},
	}})
	signals := &recordingIdentityMapSignaler{mu: sync.Mutex{}, count: 0}
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, newCaptureCache(), signals)
	flags := &feature.InMemory{}
	switch flag {
	case "on":
		signins.EnableForTests(flags, organizationID)
	case "off":
		flags.SetFlag(feature.FlagSignInEnforcement, organizationID, false)
	}
	activity.SetSignInEnforcement(flags, testenv.NewMeterProvider(t), nil)

	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	activity.WaitForDeliveries()

	if flag == "off" {
		live, err := signins.LiveSignInForSession(ctx, conn, organizationID, userID, workosSessionID)
		require.NoError(t, err)
		require.True(t, live.Valid, "deprovisioning leaves sign-ins alone while enforcement is off")
		require.Empty(t, workosClient.RevokedSessions())
		return
	}
	live, err := signins.LiveSignInForSession(ctx, conn, organizationID, userID, workosSessionID)
	require.NoError(t, err)
	require.False(t, live.Valid, "deprovisioning revokes the user's sign-ins")
	require.Equal(t, []string{workosSessionID}, workosClient.RevokedSessions(), "the revoked sign-in's WorkOS session is ended")
	audited, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAccessMemberSignInsRevoke)
	require.NoError(t, err)
	require.Equal(t, int64(1), audited, "deprovisioning audits the revocation as the system")
}

func TestProcessWorkOSOrganizationEvents_DirectoryUserReactivationRestoresDirectoryUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_directory_user_reactivate")
	logger := testenv.NewLogger(t)

	const (
		organizationID  = "gram_org_dsync_reactivate"
		workosOrgID     = "org_01HZDSYNCREACT"
		directoryUserID = "directory_user_reactivate"
		email           = "dsync.reactivate@example.com"
	)
	seedDirectoryAttributesWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_0001", Event: "dsync.user.created", CreatedAt: time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC), Data: directoryUserEventDataWithState(workosOrgID, directoryUserID, email, "active", "2026-05-12T12:00:00Z")},
		{ID: "event_0002", Event: "dsync.user.updated", CreatedAt: time.Date(2026, 5, 12, 13, 0, 0, 0, time.UTC), Data: directoryUserEventDataWithState(workosOrgID, directoryUserID, email, "inactive", "2026-05-12T13:00:00Z")},
		// Re-provisioning: an explicitly active update restores the row.
		{ID: "event_0003", Event: "dsync.user.updated", CreatedAt: time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC), Data: directoryUserEventDataWithState(workosOrgID, directoryUserID, email, "active", "2026-05-12T14:00:00Z")},
	}})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache, nil)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_0003", res.LastEventID)

	directoryUser, err := directoryrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, directoryUserID)
	require.NoError(t, err)
	require.False(t, directoryUser.Deleted)
	require.Equal(t, "event_0003", directoryUser.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_DirectoryUserStatelessUpdateDoesNotResurrect(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_directory_user_stateless")
	logger := testenv.NewLogger(t)

	const (
		organizationID  = "gram_org_dsync_stateless"
		workosOrgID     = "org_01HZDSYNCSTATELESS"
		directoryUserID = "directory_user_stateless"
		email           = "dsync.stateless@example.com"
	)
	seedDirectoryAttributesWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_0001", Event: "dsync.user.created", CreatedAt: time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC), Data: directoryUserEventDataWithState(workosOrgID, directoryUserID, email, "active", "2026-05-12T12:00:00Z")},
		{ID: "event_0002", Event: "dsync.user.updated", CreatedAt: time.Date(2026, 5, 12, 13, 0, 0, 0, time.UTC), Data: directoryUserEventDataWithState(workosOrgID, directoryUserID, email, "inactive", "2026-05-12T13:00:00Z")},
		// A newer update without a state must not resurrect the soft-deleted
		// row.
		{ID: "event_0003", Event: "dsync.user.updated", CreatedAt: time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC), Data: directoryUserEventDataWithoutState(workosOrgID, directoryUserID, email, "2026-05-12T14:00:00Z")},
	}})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache, nil)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_0003", res.LastEventID)

	_, err = directoryrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, directoryUserID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestProcessWorkOSOrganizationEvents_DirectoryEventsRaiseReviewItemsInTheirOrganization(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_directory_review_events")
	logger := testenv.NewLogger(t)

	const (
		gramOrgID   = "gram_directory_review_org"
		workosOrgID = "org_directory_review"
		otherOrgID  = "gram_directory_review_other_org"
	)
	seedDirectoryAttributesWorkOSOrganization(t, ctx, conn, gramOrgID, workosOrgID)
	seedDirectoryAttributesWorkOSOrganization(t, ctx, conn, otherOrgID, "org_directory_review_other")
	// department already has a mapping, so a new value of it needs a decision.
	_, err := accessrepo.New(conn).InsertDirectoryAttributeRoleMapping(ctx, accessrepo.InsertDirectoryAttributeRoleMappingParams{
		OrganizationID: gramOrgID, AttributeKey: conv.ToPGText("department"), AttributeValue: conv.ToPGText("Sales"),
		RoleUrn: "role:organization:00000000-0000-0000-0000-000000000001",
	})
	require.NoError(t, err)
	for _, orgID := range []string{gramOrgID, otherOrgID} {
		_, err = directoryreview.ReconcileOrganization(ctx, conn, orgID)
		require.NoError(t, err, "records the review baseline")
	}

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_0001", Event: "dsync.group.created", CreatedAt: time.Now(), Data: directoryGroupEventData(workosOrgID, "directory_group_review", "Platform")},
		{ID: "event_0002", Event: "dsync.user.created", CreatedAt: time.Now(), Data: directoryUserEventDataWithState(workosOrgID, "directory_user_review", "dsync.review@example.com", "active", "2026-05-12T12:00:00Z")},
	}})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache, nil)
	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	items, err := testrepo.New(conn).ListDirectoryReviewItemsFixture(ctx, gramOrgID)
	require.NoError(t, err)
	// The baseline raises the Sales mapping, which nobody holds, as missing;
	// the two events raise the rest.
	require.Len(t, items, 3)
	require.Equal(t, directoryreview.KindSourceMissing, items[0].Kind)
	require.Equal(t, "department=Sales", items[0].Source)
	require.Equal(t, directoryreview.KindNewGroup, items[1].Kind)
	require.Equal(t, "directory_group_review", items[1].Source)
	require.Equal(t, directoryreview.KindNewAttributeValue, items[2].Kind)
	require.Equal(t, "department=Engineering", items[2].Source)

	other, err := testrepo.New(conn).ListDirectoryReviewItemsFixture(ctx, otherOrgID)
	require.NoError(t, err)
	require.Empty(t, other, "another organization's review is untouched")
}
