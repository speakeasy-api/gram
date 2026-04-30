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
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
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

func newWorkOSEventsActivity(t *testing.T, eventsClient activities.EventsLister) (*activities.ProcessWorkOSOrganizationEvents, *pgxpool.Pool) {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, "workos_org_events_test")
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, eventsClient)
	return activity, conn
}

func emptyEventsResponse() events.ListEventsResponse {
	return events.ListEventsResponse{Data: []events.Event{}}
}
