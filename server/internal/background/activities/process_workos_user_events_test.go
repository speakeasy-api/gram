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

func TestProcessWorkOSUserEvents_LinksOptimisticRoleAssignments(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newUserEventsTestConn(t, "workos_user_events_link_role_assignments")
	logger := testenv.NewLogger(t)

	const workosUserID = "user_role_assignment"
	gramID := users.UserIDFromWorkOSID(workosUserID)
	seedTime := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)

	_, err := orgrepo.New(conn).UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:                "org_role_assignment",
		Name:              "Role Assignment Org",
		Slug:              "role-assignment-org",
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

	stub := &stubWorkOSEventsClient{pages: [][]events.Event{{
		{ID: "event_user_role_assignment", Event: "user.created", CreatedAt: time.Now(), Data: userEventData(workosUserID, "role@example.com", "Role", "User", "")},
	}}}

	activity := activities.NewProcessWorkOSUserEvents(logger, conn, stub, &stubWorkOSUserClient{})
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
