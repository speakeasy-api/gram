package activities_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
)

type stubWorkOSEventsClient struct {
	pages [][]events.Event
	calls []events.ListEventsOpts
}

func (s *stubWorkOSEventsClient) ListEvents(_ context.Context, opts events.ListEventsOpts) (events.ListEventsResponse, error) {
	s.calls = append(s.calls, opts)

	idx := len(s.calls) - 1
	if idx >= len(s.pages) {
		return events.ListEventsResponse{Data: nil}, nil
	}
	return events.ListEventsResponse{Data: s.pages[idx]}, nil
}

func newOrgEventsTestConn(t *testing.T, name string) *pgxpool.Pool {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	return conn
}

func newOrgEventPayload(t *testing.T, workosOrgID string) []byte {
	t.Helper()
	return []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Test","external_id":""}`)
}

func TestProcessWorkOSOrganizationEvents_AdvancesCursor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_advance_cursor")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZTESTADVANCE"

	stub := &stubWorkOSEventsClient{
		pages: [][]events.Event{
			{
				{ID: "event_01HZA", Event: "organization.updated", CreatedAt: time.Now(), Data: newOrgEventPayload(t, workosOrgID)},
				{ID: "event_01HZB", Event: "organization.updated", CreatedAt: time.Now(), Data: newOrgEventPayload(t, workosOrgID)},
			},
		},
	}

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
	})
	require.NoError(t, err)
	require.Empty(t, res.SinceEventID)
	require.Equal(t, "event_01HZB", res.LastEventID)
	require.False(t, res.HasMore)

	cursor, err := workosrepo.New(conn).GetOrganizationSyncLastEventID(ctx, workosOrgID)
	require.NoError(t, err)
	require.Equal(t, "event_01HZB", cursor)
}

func TestProcessWorkOSOrganizationEvents_ResumesFromCursor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_resume")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZTESTRESUME"

	_, err := workosrepo.New(conn).SetOrganizationSyncLastEventID(ctx, workosrepo.SetOrganizationSyncLastEventIDParams{
		WorkosOrganizationID: workosOrgID,
		LastEventID:          "event_01HZSEED",
	})
	require.NoError(t, err)

	stub := &stubWorkOSEventsClient{
		pages: [][]events.Event{
			{{ID: "event_01HZNEXT", Event: "organization.updated", CreatedAt: time.Now(), Data: newOrgEventPayload(t, workosOrgID)}},
		},
	}

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
	})
	require.NoError(t, err)
	require.Equal(t, "event_01HZSEED", res.SinceEventID)
	require.Equal(t, "event_01HZNEXT", res.LastEventID)

	require.Len(t, stub.calls, 1)
	require.Equal(t, "event_01HZSEED", stub.calls[0].After)
}

func TestProcessWorkOSOrganizationEvents_FullPageHasMore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_hasmore")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZTESTHASMORE"

	page := make([]events.Event, 100)
	for i := range page {
		page[i] = events.Event{
			ID:        "event_full_" + string(rune('A'+i%26)) + string(rune('0'+i/26)),
			Event:     "organization.updated",
			CreatedAt: time.Now(),
			Data:      newOrgEventPayload(t, workosOrgID),
		}
	}

	stub := &stubWorkOSEventsClient{pages: [][]events.Event{page}}
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.True(t, res.HasMore)
}

func TestProcessWorkOSOrganizationEvents_EmptyPage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_empty")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZTESTEMPTY"

	stub := &stubWorkOSEventsClient{pages: [][]events.Event{nil}}
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Empty(t, res.LastEventID)
	require.False(t, res.HasMore)

	_, err = workosrepo.New(conn).GetOrganizationSyncLastEventID(ctx, workosOrgID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestProcessWorkOSOrganizationEvents_RejectsMalformedEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_malformed")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZTESTBAD"

	stub := &stubWorkOSEventsClient{
		pages: [][]events.Event{{
			{ID: "event_01HZBAD", Event: "organization.updated", CreatedAt: time.Now(), Data: []byte(`{"id":"","object":"organization"}`)},
		}},
	}

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.Error(t, err)
}
