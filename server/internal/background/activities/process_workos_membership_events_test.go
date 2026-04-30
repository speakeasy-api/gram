package activities_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func newWorkOSMembershipEventsActivity(t *testing.T, eventsClient activities.EventsLister) (*activities.ProcessWorkOSMembershipEvents, *pgxpool.Pool) {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "workos_mem_events_test")
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	activity := activities.NewProcessWorkOSMembershipEvents(logger, conn, eventsClient)
	return activity, conn
}

func TestProcessWorkOSMembershipEvents_CursorResumesFromDB(t *testing.T) {
	t.Parallel()

	const storedCursor = "evt_user_cursor"

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.MatchedBy(func(o events.ListEventsOpts) bool {
		return o.After == storedCursor && o.OrganizationId == ""
	})).Return(emptyEventsResponse(), nil).Once()

	activity, db := newWorkOSMembershipEventsActivity(t, eventsClient)
	ctx := t.Context()

	_, err := workosrepo.New(db).SetUserSyncLastEventID(ctx, storedCursor)
	require.NoError(t, err)

	res, err := activity.Do(ctx, activities.ProcessWorkOSMembershipEventsParams{})
	require.NoError(t, err)
	require.Equal(t, storedCursor, res.SinceEventID)
}

func TestProcessWorkOSMembershipEvents_NoOrganizationFilter(t *testing.T) {
	t.Parallel()

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.MatchedBy(func(o events.ListEventsOpts) bool {
		return o.OrganizationId == ""
	})).Return(emptyEventsResponse(), nil).Once()

	activity, _ := newWorkOSMembershipEventsActivity(t, eventsClient)
	ctx := t.Context()

	_, err := activity.Do(ctx, activities.ProcessWorkOSMembershipEventsParams{})
	require.NoError(t, err)
}

func TestProcessWorkOSMembershipEvents_UnknownOrgSkips(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_unknown_mem"
	const workosUserID = "wos_user_unknown_org"

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.Anything).
		Return(events.ListEventsResponse{Data: []events.Event{
			newWorkOSMembershipEvent(t, "organization_membership.created", "evt_mem_1", "mem_1", workosOrgID, workosUserID, "member"),
		}}, nil).Once()

	activity, _ := newWorkOSMembershipEventsActivity(t, eventsClient)
	ctx := t.Context()

	_, err := activity.Do(ctx, activities.ProcessWorkOSMembershipEventsParams{})
	require.NoError(t, err)
}

func TestProcessWorkOSMembershipEvents_UnknownUserStoresRoleAssignment(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_unknownuser_mem"
	const gramOrgID = "gram_org_unknownuser_mem"
	const workosUserID = "wos_user_nonexistent_mem"

	// Seed the role separately via direct DB insert (would normally come from org events workflow).
	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.Anything).
		Return(events.ListEventsResponse{Data: []events.Event{
			newWorkOSMembershipEvent(t, "organization_membership.created", "evt_mem_2", "mem_2", workosOrgID, workosUserID, "member"),
		}}, nil).Once()

	activity, db := newWorkOSMembershipEventsActivity(t, eventsClient)
	ctx := t.Context()

	_, err := orgrepo.New(db).UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:       gramOrgID,
		Name:     "Org",
		Slug:     "org",
		WorkosID: conv.ToPGText(workosOrgID),
	})
	require.NoError(t, err)

	_, err = activity.Do(ctx, activities.ProcessWorkOSMembershipEventsParams{})
	require.NoError(t, err)

	cursor, err := workosrepo.New(db).GetUserSyncLastEventID(ctx)
	require.NoError(t, err)
	require.Equal(t, "evt_mem_2", cursor)

	userRoles, err := orgrepo.New(db).GetOrganizationUserRoles(ctx, orgrepo.GetOrganizationUserRolesParams{
		OrganizationID: gramOrgID,
		UserID:         conv.ToPGText(""),
	})
	require.NoError(t, err)
	require.Empty(t, userRoles)
}

func TestProcessWorkOSMembershipEvents_LinksUserWhenRoleAlreadySeeded(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_mem_link"
	const gramOrgID = "gram_org_mem_link"
	const gramUserID = "gram_user_mem_link"
	const workosUserID = "wos_user_mem_link"

	// First run org events activity to seed the role.
	orgEventsClient := newMockEventsLister(t)
	orgEventsClient.On("ListEvents", mock.Anything, mock.Anything).
		Return(events.ListEventsResponse{Data: []events.Event{
			newWorkOSRoleEvent(t, "organization_role.created", "evt_role_member", "role_member", workosOrgID, "member", "Member"),
		}}, nil).Once()

	orgActivity, db := newWorkOSEventsActivity(t, orgEventsClient)
	ctx := t.Context()

	_, err := orgrepo.New(db).UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:       gramOrgID,
		Name:     "Org",
		Slug:     "org",
		WorkosID: conv.ToPGText(workosOrgID),
	})
	require.NoError(t, err)

	_, err = usersrepo.New(db).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          gramUserID,
		Email:       "user-mem-link@example.com",
		DisplayName: "Test User",
		PhotoUrl:    conv.ToPGText(""),
		Admin:       false,
	})
	require.NoError(t, err)
	err = usersrepo.New(db).SetUserWorkosID(ctx, usersrepo.SetUserWorkosIDParams{
		WorkosID: conv.ToPGText(workosUserID),
		ID:       gramUserID,
	})
	require.NoError(t, err)

	_, err = orgActivity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
	})
	require.NoError(t, err)

	// Now run the membership events activity — same DB connection via separate activity instance.
	memEventsClient := newMockEventsLister(t)
	memEventsClient.On("ListEvents", mock.Anything, mock.Anything).
		Return(events.ListEventsResponse{Data: []events.Event{
			newWorkOSMembershipEvent(t, "organization_membership.created", "evt_mem_link", "mem_link_1", workosOrgID, workosUserID, "member"),
		}}, nil).Once()

	logger := testenv.NewLogger(t)
	memActivity := activities.NewProcessWorkOSMembershipEvents(logger, db, memEventsClient)

	_, err = memActivity.Do(ctx, activities.ProcessWorkOSMembershipEventsParams{})
	require.NoError(t, err)

	linked, err := orgrepo.New(db).HasOrganizationUserRelationship(ctx, orgrepo.HasOrganizationUserRelationshipParams{
		OrganizationID: gramOrgID,
		UserID:         gramUserID,
	})
	require.NoError(t, err)
	require.True(t, linked)

	userRoles, err := orgrepo.New(db).GetOrganizationUserRoles(ctx, orgrepo.GetOrganizationUserRolesParams{
		OrganizationID: gramOrgID,
		UserID:         conv.ToPGText(gramUserID),
	})
	require.NoError(t, err)
	require.Len(t, userRoles, 1)
	require.Equal(t, "member", userRoles[0].WorkosSlug)
}
