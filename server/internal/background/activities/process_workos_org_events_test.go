package activities_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
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

func TestProcessWorkOSOrganizationEvents_CursorResumesFromDB(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_cursor"
	const storedCursor = "evt_stored_cursor"

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.MatchedBy(func(o events.ListEventsOpts) bool {
		return o.After == storedCursor && o.OrganizationId == workosOrgID
	})).Return(emptyEventsResponse(), nil).Once()

	activity, db := newWorkOSEventsActivity(t, eventsClient)
	ctx := t.Context()

	_, err := workosrepo.New(db).SetOrganizationSyncLastEventID(ctx, workosrepo.SetOrganizationSyncLastEventIDParams{
		WorkosOrganizationID: workosOrgID,
		LastEventID:          storedCursor,
	})
	require.NoError(t, err)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
	})
	require.NoError(t, err)
	require.Equal(t, storedCursor, res.SinceEventID)
}

func TestProcessWorkOSOrganizationEvents_SinceEventIDParamOverridesDBCursor(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_override"
	const storedCursor = "evt_db_cursor"
	const overrideCursor = "evt_override_cursor"

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.MatchedBy(func(o events.ListEventsOpts) bool {
		return o.After == overrideCursor
	})).Return(emptyEventsResponse(), nil).Once()

	activity, db := newWorkOSEventsActivity(t, eventsClient)
	ctx := t.Context()

	_, err := workosrepo.New(db).SetOrganizationSyncLastEventID(ctx, workosrepo.SetOrganizationSyncLastEventIDParams{
		WorkosOrganizationID: workosOrgID,
		LastEventID:          storedCursor,
	})
	require.NoError(t, err)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
		SinceEventID:         &[]string{overrideCursor}[0],
	})
	require.NoError(t, err)
	require.Equal(t, overrideCursor, res.SinceEventID)
}

func TestProcessWorkOSOrganizationEvents_FullPageSetsHasMore(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_hasmore"
	const externalID = "gram_org_hasmore"

	evts := make([]events.Event, 100)
	for i := range evts {
		evts[i] = newWorkOSOrgEvent(t, "organization.updated", fmt.Sprintf("evt_%03d", i), workosOrgID, "Org", externalID)
	}

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.Anything).
		Return(events.ListEventsResponse{Data: evts}, nil).Once()

	activity, db := newWorkOSEventsActivity(t, eventsClient)
	ctx := t.Context()

	_, err := orgrepo.New(db).UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:       externalID,
		Name:     "Org",
		Slug:     "org",
		WorkosID: conv.ToPGText(workosOrgID),
	})
	require.NoError(t, err)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
	})
	require.NoError(t, err)
	require.True(t, res.HasMore)
}

func TestProcessWorkOSOrganizationEvents_PartialPageClearsHasMore(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_nomore"
	const externalID = "gram_org_nomore"

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.Anything).
		Return(events.ListEventsResponse{Data: []events.Event{
			newWorkOSOrgEvent(t, "organization.updated", "evt_only", workosOrgID, "Org", externalID),
		}}, nil).Once()

	activity, db := newWorkOSEventsActivity(t, eventsClient)
	ctx := t.Context()

	_, err := orgrepo.New(db).UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:       externalID,
		Name:     "Org",
		Slug:     "org",
		WorkosID: conv.ToPGText(workosOrgID),
	})
	require.NoError(t, err)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
	})
	require.NoError(t, err)
	require.False(t, res.HasMore)
}

func TestProcessWorkOSOrganizationEvents_OrgCreatedMissingExternalID_Error(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_noextid"

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.Anything).
		Return(events.ListEventsResponse{Data: []events.Event{
			newWorkOSOrgEvent(t, "organization.created", "evt_create", workosOrgID, "Org", ""),
		}}, nil).Once()

	activity, _ := newWorkOSEventsActivity(t, eventsClient)
	ctx := t.Context()

	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
	})
	require.Error(t, err)
}

func TestProcessWorkOSOrganizationEvents_OrgCreatedLinksViaExternalID(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_extlink"
	const externalID = "gram_org_extlink"

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.Anything).
		Return(events.ListEventsResponse{Data: []events.Event{
			newWorkOSOrgEvent(t, "organization.created", "evt_create_link", workosOrgID, "Linked Org", externalID),
		}}, nil).Once()

	activity, db := newWorkOSEventsActivity(t, eventsClient)
	ctx := t.Context()

	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
	})
	require.NoError(t, err)

	orgID, err := orgrepo.New(db).GetOrganizationIDByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, externalID, orgID)
}

func TestProcessWorkOSOrganizationEvents_AlreadyLinkedOrgIsIdempotent(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_idempotent"
	const gramOrgID = "gram_org_idempotent"

	evt := newWorkOSOrgEvent(t, "organization.updated", "evt_update", workosOrgID, "My Org", gramOrgID)

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.Anything).
		Return(events.ListEventsResponse{Data: []events.Event{evt}}, nil).Times(2)

	activity, db := newWorkOSEventsActivity(t, eventsClient)
	ctx := t.Context()

	_, err := orgrepo.New(db).UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:       gramOrgID,
		Name:     "My Org",
		Slug:     "my-org",
		WorkosID: conv.ToPGText(workosOrgID),
	})
	require.NoError(t, err)

	for range 2 {
		_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
			WorkOSOrganizationID: workosOrgID,
		})
		require.NoError(t, err)
	}

	orgID, err := orgrepo.New(db).GetOrganizationIDByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, gramOrgID, orgID)
}

func TestProcessWorkOSOrganizationEvents_CursorAdvancedToLastEvent(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_advance"
	const externalID = "gram_org_advance"

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.Anything).
		Return(events.ListEventsResponse{Data: []events.Event{
			newWorkOSOrgEvent(t, "organization.updated", "evt_a", workosOrgID, "Org", externalID),
			newWorkOSOrgEvent(t, "organization.updated", "evt_b", workosOrgID, "Org", externalID),
		}}, nil).Once()

	activity, db := newWorkOSEventsActivity(t, eventsClient)
	ctx := t.Context()

	_, err := orgrepo.New(db).UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:       externalID,
		Name:     "Org",
		Slug:     "org",
		WorkosID: conv.ToPGText(workosOrgID),
	})
	require.NoError(t, err)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
	})
	require.NoError(t, err)
	require.Equal(t, "evt_b", res.LastEventID)

	cursor, err := workosrepo.New(db).GetOrganizationSyncLastEventID(ctx, workosOrgID)
	require.NoError(t, err)
	require.Equal(t, "evt_b", cursor)
}

func TestProcessWorkOSOrganizationEvents_MembershipUnknownOrgSkips(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_unknown_mem"
	const workosUserID = "wos_user_unknown_org"

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.Anything).
		Return(events.ListEventsResponse{Data: []events.Event{
			newWorkOSMembershipEvent(t, "organization_membership.created", "evt_mem_1", "mem_1", workosOrgID, workosUserID, "member"),
		}}, nil).Once()

	activity, _ := newWorkOSEventsActivity(t, eventsClient)
	ctx := t.Context()

	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
	})
	require.NoError(t, err)
}

func TestProcessWorkOSOrganizationEvents_MembershipUnknownUserSkips(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_unknownuser"
	const gramOrgID = "gram_org_unknownuser"
	const workosUserID = "wos_user_nonexistent"

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.Anything).
		Return(events.ListEventsResponse{Data: []events.Event{
			newWorkOSMembershipEvent(t, "organization_membership.created", "evt_mem_2", "mem_2", workosOrgID, workosUserID, "member"),
		}}, nil).Once()

	activity, db := newWorkOSEventsActivity(t, eventsClient)
	ctx := t.Context()

	_, err := orgrepo.New(db).UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:       gramOrgID,
		Name:     "Org",
		Slug:     "org",
		WorkosID: conv.ToPGText(workosOrgID),
	})
	require.NoError(t, err)

	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
	})
	require.NoError(t, err)
}

func TestProcessWorkOSOrganizationEvents_MembershipCreatedLinksUser(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_mem_link"
	const gramOrgID = "gram_org_mem_link"
	const gramUserID = "gram_user_mem_link"
	const workosUserID = "wos_user_mem_link"

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.Anything).
		Return(events.ListEventsResponse{Data: []events.Event{
			newWorkOSRoleEvent(t, "organization_role.created", "evt_role_member", "role_member", workosOrgID, "member", "Member"),
			newWorkOSMembershipEvent(t, "organization_membership.created", "evt_mem_link", "mem_link_1", workosOrgID, workosUserID, "member"),
		}}, nil).Once()

	membershipLister := newMockMembershipLister(t)
	membershipLister.On("ListOrgMemberships", mock.Anything, workosOrgID).
		Return([]workos.Member{}, nil).Once()

	activity, db := newWorkOSEventsActivityWithMemberships(t, eventsClient, membershipLister)
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
		Email:       "user@example.com",
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

	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
	})
	require.NoError(t, err)

	linked, err := orgrepo.New(db).HasOrganizationUserRelationship(ctx, orgrepo.HasOrganizationUserRelationshipParams{
		OrganizationID: gramOrgID,
		UserID:         gramUserID,
	})
	require.NoError(t, err)
	require.True(t, linked)

	userRoles, err := orgrepo.New(db).GetOrganizationUserRoles(ctx, orgrepo.GetOrganizationUserRolesParams{
		OrganizationID: gramOrgID,
		UserID:         gramUserID,
	})
	require.NoError(t, err)
	require.Len(t, userRoles, 1)
	require.Equal(t, "member", userRoles[0].WorkosSlug)
}

func TestProcessWorkOSOrganizationEvents_MembershipBeforeRoleEventSilentlySkipsRoleAssignment(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_mem_missing_role"
	const gramOrgID = "gram_org_mem_missing_role"
	const gramUserID = "gram_user_mem_missing_role"
	const workosUserID = "wos_user_mem_missing_role"

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.Anything).
		Return(events.ListEventsResponse{Data: []events.Event{
			newWorkOSMembershipEvent(t, "organization_membership.created", "evt_mem_missing_role", "mem_missing_role_1", workosOrgID, workosUserID, "member"),
		}}, nil).Once()

	activity, db := newWorkOSEventsActivity(t, eventsClient)
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
		Email:       "missing-role@example.com",
		DisplayName: "Missing Role",
		PhotoUrl:    conv.ToPGText(""),
		Admin:       false,
	})
	require.NoError(t, err)
	err = usersrepo.New(db).SetUserWorkosID(ctx, usersrepo.SetUserWorkosIDParams{
		WorkosID: conv.ToPGText(workosUserID),
		ID:       gramUserID,
	})
	require.NoError(t, err)

	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
	})
	require.NoError(t, err)

	cursor, err := workosrepo.New(db).GetOrganizationSyncLastEventID(ctx, workosOrgID)
	require.NoError(t, err)
	require.Equal(t, "evt_mem_missing_role", cursor)

	userRoles, err := orgrepo.New(db).GetOrganizationUserRoles(ctx, orgrepo.GetOrganizationUserRolesParams{
		OrganizationID: gramOrgID,
		UserID:         gramUserID,
	})
	require.NoError(t, err)
	require.Empty(t, userRoles)
}

func TestProcessWorkOSOrganizationEvents_RoleUpdatedSyncsMemberships(t *testing.T) {
	t.Parallel()

	const workosOrgID = "wos_org_role_updated"
	const gramOrgID = "gram_org_role_updated"
	const gramUserID = "gram_user_role_updated"
	const workosUserID = "wos_user_role_updated"

	membershipLister := newMockMembershipLister(t)
	// Called once for role.created and once for role.updated.
	membershipLister.On("ListOrgMemberships", mock.Anything, workosOrgID).
		Return([]workos.Member{
			{UserID: workosUserID, RoleSlugs: []string{"member"}},
		}, nil).Times(2)

	eventsClient := newMockEventsLister(t)
	eventsClient.On("ListEvents", mock.Anything, mock.Anything).
		Return(events.ListEventsResponse{Data: []events.Event{
			newWorkOSRoleEvent(t, "organization_role.created", "evt_role_create", "role_member", workosOrgID, "member", "Member"),
			newWorkOSRoleEvent(t, "organization_role.updated", "evt_role_update", "role_member", workosOrgID, "member", "Member Updated"),
		}}, nil).Once()

	activity, db := newWorkOSEventsActivityWithMemberships(t, eventsClient, membershipLister)
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
		Email:       "role-updated@example.com",
		DisplayName: "Role Updated User",
		PhotoUrl:    conv.ToPGText(""),
		Admin:       false,
	})
	require.NoError(t, err)
	err = usersrepo.New(db).SetUserWorkosID(ctx, usersrepo.SetUserWorkosIDParams{
		WorkosID: conv.ToPGText(workosUserID),
		ID:       gramUserID,
	})
	require.NoError(t, err)

	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
	})
	require.NoError(t, err)

	userRoles, err := orgrepo.New(db).GetOrganizationUserRoles(ctx, orgrepo.GetOrganizationUserRolesParams{
		OrganizationID: gramOrgID,
		UserID:         gramUserID,
	})
	require.NoError(t, err)
	require.Len(t, userRoles, 1)
	require.Equal(t, "member", userRoles[0].WorkosSlug)
}

type MockEventsLister struct {
	mock.Mock
}

func newMockEventsLister(t *testing.T) *MockEventsLister {
	t.Helper()
	m := &MockEventsLister{}
	t.Cleanup(func() { require.True(t, m.AssertExpectations(t)) })
	return m
}

func (m *MockEventsLister) ListEvents(ctx context.Context, opts events.ListEventsOpts) (events.ListEventsResponse, error) {
	args := m.Called(ctx, opts)
	resp, _ := args.Get(0).(events.ListEventsResponse)
	return resp, mockErr(args, 1)
}

type MockMembershipLister struct {
	mock.Mock
}

func newMockMembershipLister(t *testing.T) *MockMembershipLister {
	t.Helper()
	m := &MockMembershipLister{}
	t.Cleanup(func() { require.True(t, m.AssertExpectations(t)) })
	return m
}

func (m *MockMembershipLister) ListOrgMemberships(ctx context.Context, orgID string) ([]workos.Member, error) {
	args := m.Called(ctx, orgID)
	members, _ := args.Get(0).([]workos.Member)
	return members, mockErr(args, 1)
}

func mockErr(args mock.Arguments, index int) error {
	if err := args.Error(index); err != nil {
		return fmt.Errorf("mock return error: %w", err)
	}
	return nil
}

func newWorkOSOrgEvent(t *testing.T, kind, eventID, workosOrgID, name, externalID string) events.Event {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"id":          workosOrgID,
		"object":      "organization",
		"name":        name,
		"external_id": externalID,
	})
	require.NoError(t, err)
	return events.Event{ID: eventID, Event: kind, Data: data, CreatedAt: time.Now()}
}

func newWorkOSMembershipEvent(t *testing.T, kind, eventID, membershipID, workosOrgID, workosUserID, roleSlug string) events.Event {
	t.Helper()
	now := time.Now()
	data, err := json.Marshal(map[string]any{
		"id":              membershipID,
		"object":          "organization_membership",
		"organization_id": workosOrgID,
		"user_id":         workosUserID,
		"role_slug":       roleSlug,
		"status":          "active",
		"created_at":      now,
		"updated_at":      now,
	})
	require.NoError(t, err)
	return events.Event{ID: eventID, Event: kind, Data: data, CreatedAt: now}
}

func newWorkOSRoleEvent(t *testing.T, kind, eventID, roleID, workosOrgID, slug, name string) events.Event {
	t.Helper()
	now := time.Now()
	data, err := json.Marshal(map[string]any{
		"id":              roleID,
		"object":          "organization_role",
		"organization_id": workosOrgID,
		"name":            name,
		"slug":            slug,
		"description":     "",
		"created_at":      now,
		"updated_at":      now,
	})
	require.NoError(t, err)
	return events.Event{ID: eventID, Event: kind, Data: data, CreatedAt: now}
}

// newWorkOSEventsActivity creates an activity with a no-op membership lister (for tests that don't exercise role events).
func newWorkOSEventsActivity(t *testing.T, eventsClient activities.EventsLister) (*activities.ProcessWorkOSOrganizationEvents, *pgxpool.Pool) {
	t.Helper()
	return newWorkOSEventsActivityWithMemberships(t, eventsClient, &noopMembershipLister{})
}

func newWorkOSEventsActivityWithMemberships(t *testing.T, eventsClient activities.EventsLister, membershipLister activities.MembershipLister) (*activities.ProcessWorkOSOrganizationEvents, *pgxpool.Pool) {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "workos_org_events_test")
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, eventsClient, membershipLister)
	return activity, conn
}

type noopMembershipLister struct{}

func (n *noopMembershipLister) ListOrgMemberships(_ context.Context, _ string) ([]workos.Member, error) {
	return nil, nil
}

func emptyEventsResponse() events.ListEventsResponse {
	return events.ListEventsResponse{Data: []events.Event{}}
}
