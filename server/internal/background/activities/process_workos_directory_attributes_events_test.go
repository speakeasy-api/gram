package activities_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
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

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache)
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

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache)
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

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache)
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

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache)
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
		RestoreDeleted:        true,
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

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_0001", res.LastEventID)

	_, _, _, deleted := getDirectoryGroupRow(t, ctx, conn, groupID)
	require.True(t, deleted)
	require.Equal(t, 0, countCurrentMemberships(t, ctx, conn, groupID, directoryUserID))
}

func directoryUserEventDataWithState(workosOrgID, workosDirectoryUserID, email, state, updatedAt string) []byte {
	return []byte(`{"id":"` + workosDirectoryUserID + `","organization_id":"` + workosOrgID + `","email":"` + email + `","first_name":"Ada","last_name":"Lovelace","state":"` + state + `","custom_attributes":{"department":"Engineering"},"created_at":"2026-05-12T10:00:00Z","updated_at":"` + updatedAt + `"}`)
}

func directoryUserEventDataWithoutState(workosOrgID, workosDirectoryUserID, email, updatedAt string) []byte {
	return []byte(`{"id":"` + workosDirectoryUserID + `","organization_id":"` + workosOrgID + `","email":"` + email + `","first_name":"Ada","last_name":"Lovelace","custom_attributes":{"department":"Engineering"},"created_at":"2026-05-12T10:00:00Z","updated_at":"` + updatedAt + `"}`)
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

	// The seeded user's email matches the directory user payload so the
	// deactivation can resolve the Gram user by email.
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
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, capturingCache)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_0003", res.LastEventID)

	// The directory user row is soft-deleted.
	_, err = workosrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, directoryUserID)
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

	// Cached user info is invalidated so org-access checks observe the
	// deprovisioning without waiting out the cache TTL.
	deletedKeys := capturingCache.Deleted()
	require.Len(t, deletedKeys, 1)
	require.Contains(t, deletedKeys[0], sessions.UserInfoCacheKey(userID))
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

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_0003", res.LastEventID)

	directoryUser, err := workosrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, directoryUserID)
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

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, workosClient, cache.NoopCache)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_0003", res.LastEventID)

	_, err = workosrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, directoryUserID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
