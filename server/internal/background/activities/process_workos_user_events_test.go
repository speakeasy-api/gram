package activities_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	"github.com/speakeasy-api/gram/server/internal/users"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func newUserEventsTestConn(t *testing.T, name string) *pgxpool.Pool {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	return conn
}

func userEventData(workosUserID, email, firstName, lastName, photoURL string) []byte {
	return []byte(`{"id":"` + workosUserID + `","email":"` + email + `","first_name":"` + firstName + `","last_name":"` + lastName + `","profile_picture_url":"` + photoURL + `","created_at":"2026-05-11T10:00:00Z","updated_at":"2026-05-12T10:00:00Z","deleted_at":"2026-05-12T10:00:00Z"}`)
}

func userEventDataWithExternalID(workosUserID, externalID, email, firstName, lastName, photoURL string) []byte {
	return []byte(`{"id":"` + workosUserID + `","external_id":"` + externalID + `","email":"` + email + `","first_name":"` + firstName + `","last_name":"` + lastName + `","profile_picture_url":"` + photoURL + `","created_at":"2026-05-11T10:00:00Z","updated_at":"2026-05-12T10:00:00Z"}`)
}

func directoryUserEventData(workosDirectoryUserID, email string) []byte {
	return []byte(`{"id":"` + workosDirectoryUserID + `","email":"` + email + `","first_name":"Ada","last_name":"Lovelace","custom_attributes":{"department":"Engineering","team":"SDK"},"updated_at":"2026-05-12T10:00:00Z"}`)
}

func directoryUserEventDataWithOrganization(workosDirectoryUserID, workosOrgID, email string) []byte {
	return []byte(`{"id":"` + workosDirectoryUserID + `","organization_id":"` + workosOrgID + `","email":"` + email + `","first_name":"Ada","last_name":"Lovelace","custom_attributes":{"department":"Engineering","team":"SDK"},"updated_at":"2026-05-12T10:00:00Z"}`)
}

func processWorkOSUserEventsParams(workosUserID string) activities.ProcessWorkOSUserEventsParams {
	return activities.ProcessWorkOSUserEventsParams{WorkOSUserID: workosUserID, SinceEventID: nil}
}

func syncedUserParams(id, workosUserID, email, displayName string) usersrepo.UpsertSyncedUserParams {
	seedTime := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	return usersrepo.UpsertSyncedUserParams{
		ID:              id,
		Email:           email,
		DisplayName:     displayName,
		PhotoUrl:        conv.ToPGTextEmpty(""),
		WorkosID:        conv.ToPGText(workosUserID),
		WorkosCreatedAt: conv.ToPGTimestamptz(seedTime),
		WorkosUpdatedAt: conv.ToPGTimestamptz(seedTime),
	}
}

func localUserParams(id, email, displayName string) usersrepo.UpsertUserParams {
	return usersrepo.UpsertUserParams{
		ID:          id,
		Email:       email,
		DisplayName: displayName,
		PhotoUrl:    conv.ToPGTextEmpty(""),
		Admin:       false,
	}
}

type failingUpdateExternalIDWorkOSClient struct {
	*workos.StubClient
	err   error
	calls int
}

func (c *failingUpdateExternalIDWorkOSClient) UpdateUserExternalID(_ context.Context, _, _ string) error {
	c.calls++
	return c.err
}

func TestProcessWorkOSUserEvents_CreatesUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_create")
	logger := testenv.NewLogger(t)

	const workosUserID = "user_create"
	gramID := users.UserIDFromWorkOSID(workosUserID)
	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_user_create", Event: "user.created", CreatedAt: time.Now(), Data: userEventData(workosUserID, "ada@example.com", "Ada", "Lovelace", "https://example.com/ada.png")},
	}})

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, processWorkOSUserEventsParams(workosUserID))
	require.NoError(t, err)
	require.Equal(t, "event_user_create", res.LastEventID)

	row, err := usersrepo.New(conn).GetUser(ctx, gramID)
	require.NoError(t, err)
	require.Equal(t, "ada@example.com", row.Email)
	require.Equal(t, "Ada Lovelace", row.DisplayName)
	require.Equal(t, workosUserID, row.WorkosID.String)
	require.True(t, row.WorkosCreatedAt.Valid)
	require.True(t, row.WorkosUpdatedAt.Valid)
	require.False(t, row.WorkosDeletedAt.Valid)
	require.False(t, row.DeletedAt.Valid)
	require.Equal(t, []workos.UserExternalIDUpdate{{WorkOSUserID: workosUserID, ExternalID: gramID}}, workosClient.UserExternalIDUpdates())
}

func TestProcessWorkOSUserEvents_StoresDirectoryUserAttributes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_directory_user_attributes")
	logger := testenv.NewLogger(t)

	const (
		workosDirectoryUserID = "directory_user_attributes"
		gramOrgID             = "gram_directory_user_attributes_org"
		workosOrgID           = "org_directory_user_attributes"
		email                 = "ada.directory@example.com"
	)
	seedDirectoryAttributesWorkOSOrganization(t, ctx, conn, gramOrgID, workosOrgID)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_directory_user_update", Event: "dsync.user.updated", CreatedAt: time.Now(), Data: directoryUserEventDataWithOrganization(workosDirectoryUserID, workosOrgID, email)},
	}})

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, processWorkOSUserEventsParams(workosDirectoryUserID))
	require.NoError(t, err)
	require.Equal(t, "event_directory_user_update", res.LastEventID)

	row, err := workosrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, workosDirectoryUserID)
	require.NoError(t, err)
	require.Equal(t, gramOrgID, row.OrganizationID)
	require.Equal(t, email, row.Email.String)
	require.JSONEq(t, `{
		"department": "Engineering",
		"team": "SDK"
	}`, string(row.Attributes))
	require.True(t, row.AttributesContentHash.Valid)
	require.Contains(t, row.AttributesContentHash.String, "sha256:")
}

func TestProcessWorkOSUserEvents_CreatesUserWithExistingExternalID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_create_external_id")
	logger := testenv.NewLogger(t)

	const workosUserID = "user_create_external_id"
	const externalID = "sb_existing_user"
	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_user_create_external_id", Event: "user.created", CreatedAt: time.Now(), Data: userEventDataWithExternalID(workosUserID, externalID, "ada@example.com", "Ada", "Lovelace", "")},
	}})

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, processWorkOSUserEventsParams(workosUserID))
	require.NoError(t, err)
	require.Equal(t, "event_user_create_external_id", res.LastEventID)

	row, err := usersrepo.New(conn).GetUser(ctx, externalID)
	require.NoError(t, err)
	require.Equal(t, "ada@example.com", row.Email)
	require.Equal(t, workosUserID, row.WorkosID.String)
	require.Empty(t, workosClient.UserExternalIDUpdates())
}

func TestProcessWorkOSUserEvents_LinksExistingLocalUserByExternalID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_link_external_id")
	logger := testenv.NewLogger(t)

	const workosUserID = "user_link_external_id"
	const externalID = "sb_existing_local_user"
	_, err := usersrepo.New(conn).UpsertUser(ctx, localUserParams(externalID, "old@example.com", "Old Name"))
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_user_link_external_id", Event: "user.updated", CreatedAt: time.Now(), Data: userEventDataWithExternalID(workosUserID, externalID, "ada@example.com", "Ada", "Lovelace", "")},
	}})

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, processWorkOSUserEventsParams(workosUserID))
	require.NoError(t, err)
	require.Equal(t, "event_user_link_external_id", res.LastEventID)

	row, err := usersrepo.New(conn).GetUser(ctx, externalID)
	require.NoError(t, err)
	require.Equal(t, "ada@example.com", row.Email)
	require.Equal(t, "Ada Lovelace", row.DisplayName)
	require.Equal(t, workosUserID, row.WorkosID.String)
	require.True(t, row.WorkosCreatedAt.Valid)
	require.True(t, row.WorkosUpdatedAt.Valid)
	require.Empty(t, workosClient.UserExternalIDUpdates())

	_, err = usersrepo.New(conn).GetUser(ctx, users.UserIDFromWorkOSID(workosUserID))
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestProcessWorkOSUserEvents_UsesExistingWorkOSLinkBeforeExternalID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_workos_first")
	logger := testenv.NewLogger(t)

	const workosUserID = "user_workos_first"
	const existingID = "sb_existing_workos_link"
	const externalID = "sb_external_should_not_win"
	_, err := usersrepo.New(conn).UpsertSyncedUser(ctx, syncedUserParams(existingID, workosUserID, "old@example.com", "Old Name"))
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_user_workos_first", Event: "user.updated", CreatedAt: time.Now(), Data: userEventDataWithExternalID(workosUserID, externalID, "new@example.com", "New", "Name", "")},
	}})

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, processWorkOSUserEventsParams(workosUserID))
	require.NoError(t, err)
	require.Equal(t, "event_user_workos_first", res.LastEventID)

	row, err := usersrepo.New(conn).GetUser(ctx, existingID)
	require.NoError(t, err)
	require.Equal(t, "new@example.com", row.Email)
	require.Equal(t, "New Name", row.DisplayName)
	require.Equal(t, workosUserID, row.WorkosID.String)
	require.Empty(t, workosClient.UserExternalIDUpdates())

	_, err = usersrepo.New(conn).GetUser(ctx, externalID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestProcessWorkOSUserEvents_ExternalIDLinkedToDifferentWorkOSUserStopsSync(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_external_id_conflict")
	logger := testenv.NewLogger(t)

	const workosUserID = "user_external_id_conflict"
	const otherWorkOSUserID = "user_external_id_owner"
	const externalID = "sb_external_id_owner"
	derivedID := users.UserIDFromWorkOSID(workosUserID)
	_, err := usersrepo.New(conn).UpsertSyncedUser(ctx, syncedUserParams(externalID, otherWorkOSUserID, "owner@example.com", "Owner User"))
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_user_external_id_conflict", Event: "user.updated", CreatedAt: time.Now(), Data: userEventDataWithExternalID(workosUserID, externalID, "new@example.com", "New", "User", "")},
	}})

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, processWorkOSUserEventsParams(workosUserID))
	require.Error(t, err)
	require.Nil(t, res)
	require.ErrorIs(t, err, oops.ErrPermanent)

	externalIDRow, err := usersrepo.New(conn).GetUser(ctx, externalID)
	require.NoError(t, err)
	require.Equal(t, otherWorkOSUserID, externalIDRow.WorkosID.String)

	_, err = usersrepo.New(conn).GetUser(ctx, derivedID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Empty(t, workosClient.UserExternalIDUpdates())

	_, err = workosrepo.New(conn).GetUserSyncLastEventID(ctx, conv.ToPGText(workosUserID))
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestProcessWorkOSUserEvents_LinksOptimisticRoleAssignments(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_link_role_assignments")
	logger := testenv.NewLogger(t)

	const workosUserID = "user_role_assignment"
	gramID := users.UserIDFromWorkOSID(workosUserID)
	seedTime := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)

	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   "org_role_assignment",
		Name: "Role Assignment Org",
		Slug: "role-assignment-org",
	})
	require.NoError(t, err)

	_, err = orgrepo.New(conn).UpdateOrganizationMetadataFromWorkOS(ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		ID:                "org_role_assignment",
		Name:              "Role Assignment Org",
		WorkosID:          conv.ToPGText("org_role_assignment"),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID: conv.ToPGText("event_org_role_assignment"),
	})
	require.NoError(t, err)
	err = accessrepo.New(conn).UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{
		WorkosSlug:        "assignment-role",
		WorkosName:        "Assignment Role",
		WorkosDescription: conv.ToPGText("Assignment Role"),
		WorkosCreatedAt:   conv.ToPGTimestamptz(seedTime),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID: conv.ToPGText("event_global_role_assignment"),
	})
	require.NoError(t, err)
	err = orgrepo.New(conn).SyncUserOrganizationRoleAssignments(ctx, orgrepo.SyncUserOrganizationRoleAssignmentsParams{
		OrganizationID:     "org_role_assignment",
		WorkosUserID:       workosUserID,
		WorkosRoleSlugs:    []string{"assignment-role"},
		UserID:             conv.ToPGTextEmpty(""),
		WorkosMembershipID: conv.ToPGTextEmpty(""),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID:  conv.ToPGText("event_assignment_optimistic"),
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_user_role_assignment", Event: "user.created", CreatedAt: time.Now(), Data: userEventData(workosUserID, "role@example.com", "Role", "User", "")},
	}})

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, processWorkOSUserEventsParams(workosUserID))
	require.NoError(t, err)
	require.Equal(t, "event_user_role_assignment", res.LastEventID)

	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: "org_role_assignment",
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.True(t, assignments[0].UserID.Valid)
	require.Equal(t, gramID, assignments[0].UserID.String)
}

func TestProcessWorkOSUserEvents_LinksMultipleOptimisticRoleAssignments(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_link_multi_role_assignments")
	logger := testenv.NewLogger(t)

	const workosUserID = "user_multi_role_link"
	gramID := users.UserIDFromWorkOSID(workosUserID)
	seedTime := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)

	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   "org_multi_role_link",
		Name: "Multi Role Link Org",
		Slug: "multi-role-link-org",
	})
	require.NoError(t, err)

	_, err = orgrepo.New(conn).UpdateOrganizationMetadataFromWorkOS(ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		ID:                "org_multi_role_link",
		Name:              "Multi Role Link Org",
		WorkosID:          conv.ToPGText("org_multi_role_link"),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID: conv.ToPGText("event_org_multi_role_link"),
	})
	require.NoError(t, err)

	// Seed two global roles
	for _, slug := range []string{"admin-role", "builder-role"} {
		err = accessrepo.New(conn).UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{
			WorkosSlug:        slug,
			WorkosName:        slug,
			WorkosDescription: conv.ToPGText(slug),
			WorkosCreatedAt:   conv.ToPGTimestamptz(seedTime),
			WorkosUpdatedAt:   conv.ToPGTimestamptz(seedTime),
			WorkosLastEventID: conv.ToPGText("event_global_" + slug),
		})
		require.NoError(t, err)
	}

	// Sync two role assignments optimistically (no user_id yet)
	err = orgrepo.New(conn).SyncUserOrganizationRoleAssignments(ctx, orgrepo.SyncUserOrganizationRoleAssignmentsParams{
		OrganizationID:     "org_multi_role_link",
		WorkosUserID:       workosUserID,
		WorkosRoleSlugs:    []string{"admin-role", "builder-role"},
		UserID:             conv.ToPGTextEmpty(""),
		WorkosMembershipID: conv.ToPGTextEmpty(""),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID:  conv.ToPGText("event_multi_optimistic"),
	})
	require.NoError(t, err)

	// Verify both assignments exist without user_id
	preAssignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: "org_multi_role_link",
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Len(t, preAssignments, 2)
	for _, a := range preAssignments {
		require.False(t, a.UserID.Valid, "user_id should be empty before user event")
	}

	// Process user.created event — should link ALL assignment rows
	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_user_multi_role_link", Event: "user.created", CreatedAt: time.Now(), Data: userEventData(workosUserID, "multirole@example.com", "Multi", "Role", "")},
	}})

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, processWorkOSUserEventsParams(workosUserID))
	require.NoError(t, err)
	require.Equal(t, "event_user_multi_role_link", res.LastEventID)

	// All assignments should now have user_id populated
	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: "org_multi_role_link",
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 2, "both role assignments should still exist")
	for _, a := range assignments {
		require.True(t, a.UserID.Valid, "user_id should be populated after user event")
		require.Equal(t, gramID, a.UserID.String)
	}
}

func TestProcessWorkOSUserEvents_LinksPendingRelationshipOverTombstone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_link_relationship_tombstone")
	logger := testenv.NewLogger(t)

	const organizationID = "org_relationship_tombstone"
	const workosOrgID = "org_workos_relationship_tombstone"
	const workosUserID = "user_relationship_tombstone"
	const gramUserID = "gram_user_relationship_tombstone"
	const firstMembershipID = "mem_relationship_tombstone_1"
	const secondMembershipID = "mem_relationship_tombstone_2"
	seedTime := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)

	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   organizationID,
		Name: "Relationship Tombstone Org",
		Slug: "relationship-tombstone-org",
	})
	require.NoError(t, err)

	_, err = orgrepo.New(conn).UpdateOrganizationMetadataFromWorkOS(ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		ID:                organizationID,
		Name:              "Relationship Tombstone Org",
		WorkosID:          conv.ToPGText(workosOrgID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID: conv.ToPGText("event_org_relationship_tombstone"),
	})
	require.NoError(t, err)
	_, err = usersrepo.New(conn).UpsertSyncedUser(ctx, syncedUserParams(gramUserID, workosUserID, "old@example.com", "Existing User"))
	require.NoError(t, err)
	err = orgrepo.New(conn).UpsertWorkOSMembership(ctx, orgrepo.UpsertWorkOSMembershipParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGText(gramUserID),
		WorkosUserID:       conv.ToPGText(workosUserID),
		WorkosMembershipID: conv.ToPGText(firstMembershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID:  conv.ToPGText("event_relationship_tombstone_1"),
	})
	require.NoError(t, err)
	err = orgrepo.New(conn).MarkWorkOSMembershipDeleted(ctx, orgrepo.MarkWorkOSMembershipDeletedParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGText(gramUserID),
		WorkosUserID:       conv.ToPGText(workosUserID),
		WorkosMembershipID: conv.ToPGText(firstMembershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(seedTime.Add(time.Hour)),
		WorkosLastEventID:  conv.ToPGText("event_relationship_tombstone_2"),
	})
	require.NoError(t, err)
	err = orgrepo.New(conn).UpsertWorkOSMembership(ctx, orgrepo.UpsertWorkOSMembershipParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGTextEmpty(""),
		WorkosUserID:       conv.ToPGText(workosUserID),
		WorkosMembershipID: conv.ToPGText(secondMembershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(seedTime.Add(2 * time.Hour)),
		WorkosLastEventID:  conv.ToPGText("event_relationship_tombstone_3"),
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_user_relationship_tombstone", Event: "user.updated", CreatedAt: time.Now(), Data: userEventDataWithExternalID(workosUserID, gramUserID, "new@example.com", "Existing", "User", "")},
	}})

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, processWorkOSUserEventsParams(workosUserID))
	require.NoError(t, err)
	require.Equal(t, "event_user_relationship_tombstone", res.LastEventID)

	relationship, err := orgrepo.New(conn).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(gramUserID),
	})
	require.NoError(t, err)
	require.False(t, relationship.Deleted)
	require.Equal(t, secondMembershipID, relationship.WorkosMembershipID.String)
	require.Equal(t, "event_relationship_tombstone_3", relationship.WorkosLastEventID.String)
}

func TestProcessWorkOSUserEvents_LinksPendingRelationshipToExistingExternalID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_link_relationship_external")
	logger := testenv.NewLogger(t)

	const organizationID = "org_relationship_external"
	const workosOrgID = "org_workos_relationship_external"
	const workosUserID = "user_relationship_external"
	const externalID = "sb_relationship_external_user"
	const membershipID = "mem_relationship_external"
	seedTime := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)

	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   organizationID,
		Name: "Relationship External Org",
		Slug: "relationship-external-org",
	})
	require.NoError(t, err)

	_, err = orgrepo.New(conn).UpdateOrganizationMetadataFromWorkOS(ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		ID:                organizationID,
		Name:              "Relationship External Org",
		WorkosID:          conv.ToPGText(workosOrgID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID: conv.ToPGText("event_org_relationship_external"),
	})
	require.NoError(t, err)
	err = orgrepo.New(conn).UpsertWorkOSMembership(ctx, orgrepo.UpsertWorkOSMembershipParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGTextEmpty(""),
		WorkosUserID:       conv.ToPGText(workosUserID),
		WorkosMembershipID: conv.ToPGText(membershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID:  conv.ToPGText("event_relationship_external_pending"),
	})
	require.NoError(t, err)
	_, err = usersrepo.New(conn).UpsertUser(ctx, localUserParams(externalID, "old@example.com", "Old Name"))
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_user_relationship_external", Event: "user.updated", CreatedAt: time.Now(), Data: userEventDataWithExternalID(workosUserID, externalID, "new@example.com", "External", "User", "")},
	}})

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, processWorkOSUserEventsParams(workosUserID))
	require.NoError(t, err)
	require.Equal(t, "event_user_relationship_external", res.LastEventID)

	relationship, err := orgrepo.New(conn).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(externalID),
	})
	require.NoError(t, err)
	require.False(t, relationship.Deleted)
	require.Equal(t, membershipID, relationship.WorkosMembershipID.String)
	require.Equal(t, workosUserID, relationship.WorkosUserID.String)
}

func TestProcessWorkOSUserEvents_UsesExistingUserIDWhenExternalIDMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_existing_id")
	logger := testenv.NewLogger(t)

	const workosUserID = "user_existing_id"
	const existingID = "sb_existing_db_user"
	_, err := usersrepo.New(conn).UpsertSyncedUser(ctx, syncedUserParams(existingID, workosUserID, "old@example.com", "Existing Name"))
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_user_existing_id", Event: "user.updated", CreatedAt: time.Now(), Data: userEventData(workosUserID, "new@example.com", "New", "Name", "")},
	}})

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, processWorkOSUserEventsParams(workosUserID))
	require.NoError(t, err)
	require.Equal(t, "event_user_existing_id", res.LastEventID)

	row, err := usersrepo.New(conn).GetUser(ctx, existingID)
	require.NoError(t, err)
	require.Equal(t, "new@example.com", row.Email)
	require.Equal(t, "New Name", row.DisplayName)
	require.Equal(t, []workos.UserExternalIDUpdate{{WorkOSUserID: workosUserID, ExternalID: existingID}}, workosClient.UserExternalIDUpdates())
}

func TestProcessWorkOSUserEvents_UpdatesExistingUserAndPreservesBlankFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_update")
	logger := testenv.NewLogger(t)

	const workosUserID = "user_update"
	gramID := users.UserIDFromWorkOSID(workosUserID)
	params := syncedUserParams(gramID, workosUserID, "old@example.com", "Existing Name")
	params.PhotoUrl = conv.ToPGText("https://example.com/old.png")
	_, err := usersrepo.New(conn).UpsertSyncedUser(ctx, params)
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_user_update", Event: "user.updated", CreatedAt: time.Now(), Data: userEventData(workosUserID, "new@example.com", "New", "Name", "")},
	}})

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	_, err = activity.Do(ctx, processWorkOSUserEventsParams(workosUserID))
	require.NoError(t, err)

	row, err := usersrepo.New(conn).GetUser(ctx, gramID)
	require.NoError(t, err)
	require.Equal(t, "new@example.com", row.Email)
	require.Equal(t, "New Name", row.DisplayName)
	require.False(t, row.PhotoUrl.Valid)
}

func TestProcessWorkOSUserEvents_SoftDeletesUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_delete")
	logger := testenv.NewLogger(t)

	const workosUserID = "user_delete"
	gramID := users.UserIDFromWorkOSID(workosUserID)
	_, err := usersrepo.New(conn).UpsertSyncedUser(ctx, syncedUserParams(gramID, workosUserID, "delete@example.com", "Delete Me"))
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_user_delete", Event: "user.deleted", CreatedAt: time.Now(), Data: userEventData(workosUserID, "delete@example.com", "", "", "")},
	}})

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	_, err = activity.Do(ctx, processWorkOSUserEventsParams(workosUserID))
	require.NoError(t, err)

	row, err := usersrepo.New(conn).GetUser(ctx, gramID)
	require.NoError(t, err)
	require.True(t, row.DeletedAt.Valid)
	require.True(t, row.WorkosDeletedAt.Valid)
}

func TestProcessWorkOSUserEvents_AdvancesAndResumesCursor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_cursor")
	logger := testenv.NewLogger(t)

	const workosUserID = "user_cursor"
	_, err := workosrepo.New(conn).SetUserSyncLastEventID(ctx, workosrepo.SetUserSyncLastEventIDParams{
		WorkosUserID: conv.ToPGText(workosUserID),
		LastEventID:  "event_seed",
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{{
		{ID: "event_next", Event: "user.created", CreatedAt: time.Now(), Data: userEventData(workosUserID, "cursor@example.com", "", "", "")},
	}})

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, processWorkOSUserEventsParams(workosUserID))
	require.NoError(t, err)
	require.Equal(t, "event_seed", res.SinceEventID)
	require.Equal(t, "event_next", res.LastEventID)
	calls := workosClient.EventCalls()
	require.Len(t, calls, 1)
	require.Equal(t, "event_seed", calls[0].After)

	cursor, err := workosrepo.New(conn).GetUserSyncLastEventID(ctx, conv.ToPGText(workosUserID))
	require.NoError(t, err)
	require.Equal(t, "event_next", cursor)
}

func TestProcessWorkOSUserEvents_EmptyPageDoesNotAdvanceCursor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_empty")
	logger := testenv.NewLogger(t)

	const workosUserID = "user_empty"
	workosClient := workos.NewStubClient()
	workosClient.SetEventPages([][]events.Event{nil})
	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, processWorkOSUserEventsParams(workosUserID))
	require.NoError(t, err)
	require.Empty(t, res.LastEventID)
	require.False(t, res.HasMore)

	_, err = workosrepo.New(conn).GetUserSyncLastEventID(ctx, conv.ToPGText(workosUserID))
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestProcessWorkOSUserEvents_ExternalIDFailureDoesNotFail(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_external_id_failure")
	logger := testenv.NewLogger(t)

	stub := workos.NewStubClient()
	stub.SetEventPages([][]events.Event{{
		{ID: "event_external_id", Event: "user.created", CreatedAt: time.Now(), Data: userEventData("user_external", "external@example.com", "", "", "")},
	}})
	workosClient := &failingUpdateExternalIDWorkOSClient{StubClient: stub, err: errors.New("boom")}

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, workosClient)
	res, err := activity.Do(ctx, processWorkOSUserEventsParams("user_external"))
	require.NoError(t, err)
	require.Equal(t, "event_external_id", res.LastEventID)
	require.Equal(t, 1, workosClient.calls)
}
