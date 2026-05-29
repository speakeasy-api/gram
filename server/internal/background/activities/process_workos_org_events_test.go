package activities_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgid "github.com/speakeasy-api/gram/server/internal/organizations/id"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func newOrgEventsTestConn(t *testing.T, name string) *pgxpool.Pool {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	return conn
}

func newOrgEventPayload(t *testing.T, workosOrgID string) []byte {
	t.Helper()
	// external_id maps to the Speakeasy organization id; the upsert relies on
	// it for orgs not yet linked via workos_id.
	return []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Test","external_id":"sb_` + workosOrgID + `","updated_at":"2026-05-06T12:00:00Z"}`)
}

func newWorkOSClientWithEvents(pages [][]events.Event) *workos.StubClient {
	client := workos.NewStubClient()
	client.SetEventPages(pages)
	return client
}

func TestProcessWorkOSOrganizationEvents_AdvancesCursor(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_advance_cursor")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZTESTADVANCE"

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{ID: "event_01HZA", Event: "organization.updated", CreatedAt: time.Now(), Data: newOrgEventPayload(t, workosOrgID)},
			{ID: "event_01HZB", Event: "organization.updated", CreatedAt: time.Now(), Data: newOrgEventPayload(t, workosOrgID)},
		},
	})

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

	stub := newWorkOSClientWithEvents([][]events.Event{
		{{ID: "event_01HZNEXT", Event: "organization.updated", CreatedAt: time.Now(), Data: newOrgEventPayload(t, workosOrgID)}},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{
		WorkOSOrganizationID: workosOrgID,
	})
	require.NoError(t, err)
	require.Equal(t, "event_01HZSEED", res.SinceEventID)
	require.Equal(t, "event_01HZNEXT", res.LastEventID)

	require.Len(t, stub.EventCalls(), 1)
	require.Equal(t, "event_01HZSEED", stub.EventCalls()[0].After)
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

	stub := newWorkOSClientWithEvents([][]events.Event{page})
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

	stub := newWorkOSClientWithEvents([][]events.Event{nil})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Empty(t, res.LastEventID)
	require.False(t, res.HasMore)

	_, err = workosrepo.New(conn).GetOrganizationSyncLastEventID(ctx, workosOrgID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestProcessWorkOSOrganizationEvents_CreatesOrgAndUpdatesWorkOSExternalIDWhenMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_unlinked")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZTESTBAD"
	organizationID := orgid.FromWorkOSID(workosOrgID)

	// The first event has no external_id, so Gram creates the org with a
	// deterministic ID and updates WorkOS after commit. The second event still
	// updates the existing workos_id-linked org even if its payload carries a
	// different external_id.
	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{ID: "event_01HZBAD", Event: "organization.updated", CreatedAt: time.Now(), Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Pending","updated_at":"2026-05-06T11:00:00Z"}`)},
			{ID: "event_01HZGOOD", Event: "organization.updated", CreatedAt: time.Now(), Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Resolved","external_id":"sb_missing","updated_at":"2026-05-06T12:00:00Z"}`)},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZGOOD", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, organizationID, row.ID)
	require.Equal(t, "Resolved", row.Name)
	require.Equal(t, "pending", row.Slug)
	require.Equal(t, []workos.OrgExternalIDUpdate{{WorkOSOrgID: workosOrgID, ExternalID: organizationID}}, stub.OrgExternalIDUpdates())

	cursor, err := workosrepo.New(conn).GetOrganizationSyncLastEventID(ctx, workosOrgID)
	require.NoError(t, err)
	require.Equal(t, "event_01HZGOOD", cursor)
}

func TestProcessWorkOSOrganizationEvents_OrganizationCreateRejectsEmptySlug(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_empty_slug")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZEMPTYSLUG"

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZEMPTYSLUG",
				Event:     "organization.created",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"!!!",` +
					`"updated_at":"2026-05-06T12:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.Error(t, err)

	_, err = orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.ErrorIs(t, err, pgx.ErrNoRows)

	_, err = workosrepo.New(conn).GetOrganizationSyncLastEventID(ctx, workosOrgID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestProcessWorkOSOrganizationEvents_OrganizationExternalIDMissingLocallyCreates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_external_id_missing_locally_creates")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZMISSINGEXTERNAL"
	const externalID = "sb_missing_external"

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZMISSINGEXTERNAL",
				Event:     "organization.created",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Missing External","external_id":"` + externalID + `",` +
					`"updated_at":"2026-05-06T12:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZMISSINGEXTERNAL", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, externalID, row.ID)
	require.Equal(t, "Missing External", row.Name)
	require.Equal(t, "missing-external", row.Slug)

	cursor, err := workosrepo.New(conn).GetOrganizationSyncLastEventID(ctx, workosOrgID)
	require.NoError(t, err)
	require.Equal(t, "event_01HZMISSINGEXTERNAL", cursor)
}

func TestProcessWorkOSOrganizationEvents_OrganizationCreateHandlesConcurrentInsert(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_concurrent_insert")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZCONCURRENT"
	const externalID = "sb_concurrent"

	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   externalID,
		Name: "Concurrent Original",
		Slug: "concurrent-original",
	})
	require.NoError(t, err)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZCONCURRENT",
				Event:     "organization.created",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Concurrent WorkOS","external_id":"` + externalID +
					`","updated_at":"2026-05-06T12:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZCONCURRENT", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, externalID, row.ID)
	require.Equal(t, "Concurrent WorkOS", row.Name)
	require.Equal(t, "concurrent-original", row.Slug)
}

func TestProcessWorkOSOrganizationEvents_OrganizationCreateAddsHashForTakenSlug(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_create_taken_slug")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZTAKENSLUG"
	organizationID := orgid.FromWorkOSID(workosOrgID)

	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   "sb_slug_owner",
		Name: "Acme",
		Slug: "acme",
	})
	require.NoError(t, err)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZTAKENSLUG",
				Event:     "organization.created",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Acme",` +
					`"updated_at":"2026-05-06T12:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZTAKENSLUG", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, organizationID, row.ID)
	require.NotEqual(t, "acme", row.Slug)
	require.True(t, strings.HasPrefix(row.Slug, "acme-"))
}

func TestProcessWorkOSOrganizationEvents_OrganizationCreateConflictSkipsStaleEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_stale_create_conflict")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZSTALECREATE"
	const externalID = "sb_stale_create"

	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   externalID,
		Name: "Newer Name",
		Slug: "newer-name",
	})
	require.NoError(t, err)
	_, err = orgrepo.New(conn).UpdateOrganizationMetadataFromWorkOS(ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		ID:                externalID,
		Name:              "Newer Name",
		WorkosID:          conv.ToPGText(workosOrgID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)),
		WorkosLastEventID: conv.ToPGText("event_01HZ0002"),
	})
	require.NoError(t, err)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZ0001",
				Event:     "organization.created",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Older Name","external_id":"` + externalID +
					`","updated_at":"2026-05-06T11:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZ0001", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationMetadata(ctx, externalID)
	require.NoError(t, err)
	require.Equal(t, "Newer Name", row.Name)
	require.Equal(t, "newer-name", row.Slug)
	require.Equal(t, "event_01HZ0002", row.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_OrganizationCreatedAndUpdated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_create")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZCREATE"
	const externalID = "sb_01HZCREATE"

	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   externalID,
		Name: "Original",
		Slug: "original",
	})
	require.NoError(t, err)

	created := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 5, 6, 11, 0, 0, 0, time.UTC)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZA",
				Event:     "organization.created",
				CreatedAt: created,
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Acme","external_id":"` + externalID +
					`","updated_at":"2026-05-06T10:00:00Z"}`),
			},
			{
				ID:        "event_01HZB",
				Event:     "organization.updated",
				CreatedAt: updated,
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Acme Inc","external_id":"` + externalID +
					`","updated_at":"2026-05-06T11:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, externalID, row.ID)
	require.Equal(t, "Acme Inc", row.Name)
	require.Equal(t, "original", row.Slug)
	require.True(t, row.WorkosLastEventID.Valid)
	require.Equal(t, "event_01HZB", row.WorkosLastEventID.String)
	require.True(t, row.WorkosUpdatedAt.Valid)
	require.True(t, row.WorkosUpdatedAt.Time.Equal(updated))
	require.False(t, row.DisabledAt.Valid)
}

func TestProcessWorkOSOrganizationEvents_OrganizationUpdatePreservesExistingSlugOnNameConflict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_slug_preserved")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_slug_preserved"
	const workosOrgID = "org_01HZSLUGPRESERVE"

	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   organizationID,
		Name: "Original Name",
		Slug: "original-name",
	})
	require.NoError(t, err)

	err = orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   "gram_org_slug_owner",
		Name: "Taken Name",
		Slug: "taken-name",
	})
	require.NoError(t, err)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZSLUG1",
				Event:     "organization.updated",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Taken Name","external_id":"` + organizationID +
					`","updated_at":"2026-05-06T12:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZSLUG1", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, organizationID, row.ID)
	require.Equal(t, "Taken Name", row.Name)
	require.Equal(t, "original-name", row.Slug)
	require.Equal(t, "event_01HZSLUG1", row.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_OrganizationUpdateDoesNotRemapExistingWorkOSLink(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_no_remap")
	logger := testenv.NewLogger(t)

	const oldOrganizationID = "gram_org_old_workos_link"
	const newOrganizationID = "gram_org_new_workos_link"
	const workosOrgID = "org_01HZREMAP"

	seedWorkOSOrganization(t, ctx, conn, oldOrganizationID, workosOrgID)
	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   newOrganizationID,
		Name: "Target Org",
		Slug: "target-org",
	})
	require.NoError(t, err)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZREMAP",
				Event:     "organization.updated",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Remapped Org","external_id":"` + newOrganizationID +
					`","updated_at":"2026-05-06T12:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZREMAP", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, oldOrganizationID, row.ID)
	require.Equal(t, "Remapped Org", row.Name)
	require.Equal(t, oldOrganizationID, row.Slug)

	targetRow, err := orgrepo.New(conn).GetOrganizationMetadata(ctx, newOrganizationID)
	require.NoError(t, err)
	require.False(t, targetRow.WorkosID.Valid)
}

func TestProcessWorkOSOrganizationEvents_OrganizationUpdateRelinksExternalIDMatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_relink_external_id")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_external_id_relink"
	const oldWorkosOrgID = "org_01HZOLDLINK"
	const newWorkosOrgID = "org_01HZNEWLINK"

	seedWorkOSOrganization(t, ctx, conn, organizationID, oldWorkosOrgID)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZRELINK",
				Event:     "organization.updated",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + newWorkosOrgID + `","object":"organization","name":"Relinked Org","external_id":"` + organizationID +
					`","updated_at":"2026-05-06T12:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: newWorkosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZRELINK", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationMetadata(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, "Relinked Org", row.Name)
	require.Equal(t, newWorkosOrgID, row.WorkosID.String)
	require.Equal(t, "event_01HZRELINK", row.WorkosLastEventID.String)

	_, err = orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(oldWorkosOrgID))
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestProcessWorkOSOrganizationEvents_OrganizationUpdateWithoutExternalIDKeepsExistingWorkOSLink(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_no_external_keeps_link")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_existing_workos_link"
	const workosOrgID = "org_01HZNOEXTERNAL"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZNOEXTERNAL",
				Event:     "organization.updated",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Still Linked",` +
					`"updated_at":"2026-05-06T12:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZNOEXTERNAL", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationMetadata(ctx, organizationID)
	require.NoError(t, err)
	require.Equal(t, "Still Linked", row.Name)
	require.True(t, row.WorkosID.Valid)
	require.Equal(t, workosOrgID, row.WorkosID.String)
	require.Equal(t, []workos.OrgExternalIDUpdate{{WorkOSOrgID: workosOrgID, ExternalID: organizationID}}, stub.OrgExternalIDUpdates())
}

func TestProcessWorkOSOrganizationEvents_OrganizationUpdateSkippedWhenStale(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_stale")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZSTALE"
	const externalID = "sb_01HZSTALE"

	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   externalID,
		Name: "Original",
		Slug: "original-stale",
	})
	require.NoError(t, err)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZ002",
				Event:     "organization.updated",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Newer","external_id":"` + externalID +
					`","updated_at":"2026-05-06T12:00:00Z"}`),
			},
			{
				// Stale replay of an older event — must not overwrite Newer.
				ID:        "event_01HZ001",
				Event:     "organization.updated",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Older","external_id":"` + externalID +
					`","updated_at":"2026-05-06T11:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, "Newer", row.Name)
	require.Equal(t, "event_01HZ002", row.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_OrganizationDeletedSetsDisabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_delete")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZDELETE"
	const externalID = "sb_01HZDELETE"

	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   externalID,
		Name: "Doomed",
		Slug: "doomed",
	})
	require.NoError(t, err)

	_, err = orgrepo.New(conn).UpdateOrganizationMetadataFromWorkOS(ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		ID:                externalID,
		Name:              "Doomed",
		WorkosID:          conv.ToPGText(workosOrgID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(time.Now()),
		WorkosLastEventID: conv.ToPGText("event_00SEED"),
	})
	require.NoError(t, err)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZDEL",
				Event:     "organization.deleted",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Doomed","external_id":"` + externalID +
					`","updated_at":"2026-05-06T12:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.True(t, row.DisabledAt.Valid)
	require.Equal(t, "event_01HZDEL", row.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_OrganizationUpdateDoesNotClearDisabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_update_preserves_disabled")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZDISABLEDUPDATE"
	const externalID = "sb_01HZDISABLEDUPDATE"

	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   externalID,
		Name: "Disabled",
		Slug: "disabled",
	})
	require.NoError(t, err)

	_, err = orgrepo.New(conn).UpdateOrganizationMetadataFromWorkOS(ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		ID:                externalID,
		Name:              "Disabled",
		WorkosID:          conv.ToPGText(workosOrgID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(time.Date(2026, 5, 6, 11, 0, 0, 0, time.UTC)),
		WorkosLastEventID: conv.ToPGText("event_01HZ001"),
	})
	require.NoError(t, err)
	_, err = orgrepo.New(conn).DisableOrganizationByWorkosID(ctx, orgrepo.DisableOrganizationByWorkosIDParams{
		WorkosID:          conv.ToPGText(workosOrgID),
		WorkosLastEventID: conv.ToPGText("event_01HZ002"),
	})
	require.NoError(t, err)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZ003",
				Event:     "organization.updated",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Still Disabled","external_id":"` + externalID +
					`","updated_at":"2026-05-06T12:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, "Still Disabled", row.Name)
	require.True(t, row.DisabledAt.Valid)
	require.Equal(t, "event_01HZ003", row.WorkosLastEventID.String)
}

func TestProcessWorkOSGlobalRoleEvents_UpsertAndDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_global_role_events_upsert_delete")
	logger := testenv.NewLogger(t)

	const slug = "platform-admin"

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZG1",
				Event:     "role.created",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"role_01","object":"role","slug":"` + slug + `","name":"Platform Admin",` +
					`"description":"env-level admin","type":"EnvironmentRole",` +
					`"created_at":"2026-05-06T10:00:00Z","updated_at":"2026-05-06T10:00:00Z"}`),
			},
			{
				ID:        "event_01HZG2",
				Event:     "role.deleted",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"role_01","object":"role","slug":"` + slug + `","name":"Platform Admin",` +
					`"type":"EnvironmentRole","created_at":"2026-05-06T10:00:00Z","updated_at":"2026-05-06T11:00:00Z",` +
					`"deleted_at":"2026-05-06T11:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSGlobalRoleEvents(logger, conn, stub)
	_, err := activity.Do(ctx, activities.ProcessWorkOSGlobalRoleEventsParams{})
	require.NoError(t, err)

	role, err := accessrepo.New(conn).GetGlobalRoleBySlug(ctx, slug)
	require.NoError(t, err)
	require.Equal(t, "Platform Admin", role.WorkosName)
	require.True(t, role.Deleted)
	require.True(t, role.WorkosDeleted)
	require.Equal(t, "event_01HZG2", role.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_OrganizationRoleUpsertAndDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_role")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZORGROLE"
	const externalID = "sb_01HZORGROLE"
	const slug = "billing-manager"

	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   externalID,
		Name: "Acme",
		Slug: "acme",
	})
	require.NoError(t, err)

	_, err = orgrepo.New(conn).UpdateOrganizationMetadataFromWorkOS(ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		ID:                externalID,
		Name:              "Acme",
		WorkosID:          conv.ToPGText(workosOrgID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(time.Now()),
		WorkosLastEventID: conv.ToPGText("event_00SEED"),
	})
	require.NoError(t, err)

	rolePrincipal := urn.NewPrincipal(urn.PrincipalTypeRole, slug)
	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: externalID,
		PrincipalUrn:   rolePrincipal,
		Scope:          "billing:read",
		Selectors:      []byte(`{"resource_kind":"*","resource_id":"*"}`),
	})
	require.NoError(t, err)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZR1",
				Event:     "organization_role.created",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"role_01","object":"role","organization_id":"` + workosOrgID +
					`","slug":"` + slug + `","name":"Billing Manager","description":"manages billing",` +
					`"type":"OrganizationRole","created_at":"2026-05-06T10:00:00Z","updated_at":"2026-05-06T10:00:00Z"}`),
			},
			{
				ID:        "event_01HZR2",
				Event:     "organization_role.deleted",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"role_01","object":"role","organization_id":"` + workosOrgID +
					`","slug":"` + slug + `","name":"Billing Manager","type":"OrganizationRole",` +
					`"created_at":"2026-05-06T10:00:00Z","updated_at":"2026-05-06T11:00:00Z","deleted_at":"2026-05-06T11:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	role, err := accessrepo.New(conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
		OrganizationID: externalID,
		WorkosSlug:     slug,
	})
	require.NoError(t, err)
	require.True(t, role.Deleted)
	require.True(t, role.WorkosDeleted)
	require.Equal(t, "event_01HZR2", role.WorkosLastEventID.String)

	grants, err := accessrepo.New(conn).GetPrincipalGrants(ctx, accessrepo.GetPrincipalGrantsParams{
		OrganizationID: externalID,
		PrincipalUrns:  []string{rolePrincipal.String()},
	})
	require.NoError(t, err)
	require.Empty(t, grants, "principal_grants should be cascade-deleted on role delete")
}

func TestProcessWorkOSOrganizationEvents_OrganizationDeletedSkippedWhenStale(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_delete_stale")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZDELSTALE"
	const externalID = "sb_01HZDELSTALE"

	// Seed org with cursor advanced past the stale delete event ID.
	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   externalID,
		Name: "Live",
		Slug: "live",
	})
	require.NoError(t, err)

	_, err = orgrepo.New(conn).UpdateOrganizationMetadataFromWorkOS(ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		ID:                externalID,
		Name:              "Live",
		WorkosID:          conv.ToPGText(workosOrgID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(time.Now()),
		WorkosLastEventID: conv.ToPGText("event_99FRESH"),
	})
	require.NoError(t, err)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZSTALEDEL",
				Event:     "organization.deleted",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Live","external_id":"` + externalID +
					`","updated_at":"2026-05-06T11:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.False(t, row.DisabledAt.Valid, "stale delete event must not disable a fresher org row")
	require.Equal(t, "event_99FRESH", row.WorkosLastEventID.String)
}

func newWorkOSMembershipEvent(t *testing.T, eventType, eventID, membershipID, workosOrgID, workosUserID string, updatedAt time.Time, roleSlugs ...string) events.Event {
	t.Helper()

	roles := make([]struct {
		Slug string `json:"slug"`
	}, 0, len(roleSlugs))
	for _, slug := range roleSlugs {
		roles = append(roles, struct {
			Slug string `json:"slug"`
		}{Slug: slug})
	}

	payload := struct {
		ID             string `json:"id"`
		Object         string `json:"object"`
		OrganizationID string `json:"organization_id"`
		UserID         string `json:"user_id"`
		Roles          []struct {
			Slug string `json:"slug"`
		} `json:"roles"`
		UpdatedAt time.Time `json:"updated_at"`
	}{
		ID:             membershipID,
		Object:         "organization_membership",
		OrganizationID: workosOrgID,
		UserID:         workosUserID,
		Roles:          roles,
		UpdatedAt:      updatedAt,
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)

	return events.Event{
		ID:        eventID,
		Event:     eventType,
		CreatedAt: updatedAt,
		Data:      data,
	}
}

func seedWorkOSOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, workosOrgID string) {
	t.Helper()

	err := orgrepo.New(conn).CreateOrganizationMetadata(ctx, orgrepo.CreateOrganizationMetadataParams{
		ID:   organizationID,
		Name: "Test Org",
		Slug: organizationID,
	})
	require.NoError(t, err)

	_, err = orgrepo.New(conn).UpdateOrganizationMetadataFromWorkOS(ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		ID:                organizationID,
		Name:              "Test Org",
		WorkosID:          conv.ToPGText(workosOrgID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)),
		WorkosLastEventID: conv.ToPGText("event_00SEED"),
	})
	require.NoError(t, err)
}

func seedWorkOSUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, userID, workosUserID string) {
	t.Helper()

	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       userID + "@example.com",
		DisplayName: "Test User",
		PhotoUrl:    conv.ToPGText(""),
		Admin:       false,
	})
	require.NoError(t, err)

	err = usersrepo.New(conn).OverwriteUserWorkosID(ctx, usersrepo.OverwriteUserWorkosIDParams{
		WorkosID: conv.ToPGText(workosUserID),
		ID:       userID,
	})
	require.NoError(t, err)
}

func seedOrganizationRole(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, slug string) accessrepo.OrganizationRole {
	t.Helper()

	eventTime := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	_, err := accessrepo.New(conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    organizationID,
		WorkosSlug:        slug,
		WorkosName:        slug,
		WorkosDescription: conv.ToPGText(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(eventTime),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(eventTime),
		WorkosLastEventID: conv.ToPGText("event_00SEED"),
	})
	require.NoError(t, err)

	role, err := accessrepo.New(conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
		OrganizationID: organizationID,
		WorkosSlug:     slug,
	})
	require.NoError(t, err)
	return role
}

func TestProcessWorkOSOrganizationEvents_MembershipFilterIncludesMembershipTypes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_membership_filter")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZMEMFILTER"

	stub := newWorkOSClientWithEvents([][]events.Event{nil})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	require.Len(t, stub.EventCalls(), 1)
	require.Equal(t, workosOrgID, stub.EventCalls()[0].OrganizationId)
	require.ElementsMatch(t, []string{
		"organization.created",
		"organization.updated",
		"organization.deleted",
		"organization_role.created",
		"organization_role.deleted",
		"organization_role.updated",
		"organization_membership.created",
		"organization_membership.updated",
		"organization_membership.deleted",
		"connection.activated",
		"connection.deactivated",
		"connection.deleted",
		"dsync.activated",
		"dsync.deleted",
	}, stub.EventCalls()[0].Events)
}

func TestProcessWorkOSOrganizationEvents_MembershipKnownUserSyncsRoles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_membership_known_user")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_mem_known"
	const workosOrgID = "org_01HZMEMKNOWN"
	const userID = "user_mem_known"
	const workosUserID = "user_01HZMEMKNOWN"

	updatedAt := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedWorkOSUser(t, ctx, conn, userID, workosUserID)
	organizationRole := seedOrganizationRole(t, ctx, conn, organizationID, "member")

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSMembershipEvent(t, "organization_membership.created", "event_01HZMEM1", "mem_01HZKNOWN", workosOrgID, workosUserID, updatedAt, "member"),
		},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZMEM1", res.LastEventID)

	relationship, err := orgrepo.New(conn).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
	require.False(t, relationship.Deleted)
	require.Equal(t, "mem_01HZKNOWN", relationship.WorkosMembershipID.String)
	require.Equal(t, "event_01HZMEM1", relationship.WorkosLastEventID.String)

	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, fmt.Sprintf("role:organization:%s", organizationRole.ID.String()), assignments[0].RoleUrn)

	cursor, err := workosrepo.New(conn).GetOrganizationSyncLastEventID(ctx, workosOrgID)
	require.NoError(t, err)
	require.Equal(t, "event_01HZMEM1", cursor)
}

func TestProcessWorkOSOrganizationEvents_MembershipUnknownUserStillSyncsRoles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_membership_unknown_user")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_mem_unknown_user"
	const workosOrgID = "org_01HZMEMUNKNOWNUSER"
	const workosUserID = "user_01HZMEMUNKNOWN"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	memberRole := seedOrganizationRole(t, ctx, conn, organizationID, "member")

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSMembershipEvent(t, "organization_membership.created", "event_01HZMEMUNK", "mem_01HZUNKNOWN", workosOrgID, workosUserID, time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC), "member"),
		},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZMEMUNK", res.LastEventID)

	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, fmt.Sprintf("role:organization:%s", memberRole.ID.String()), assignments[0].RoleUrn)
	require.False(t, assignments[0].UserID.Valid)
}

func TestProcessWorkOSOrganizationEvents_MembershipDeleteSoftDeletesAndClearsAssignments(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_membership_delete")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_mem_delete"
	const workosOrgID = "org_01HZMEMDELETE"
	const userID = "user_mem_delete"
	const workosUserID = "user_01HZMEMDELETE"
	const membershipID = "mem_01HZDELETE"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedWorkOSUser(t, ctx, conn, userID, workosUserID)
	seedOrganizationRole(t, ctx, conn, organizationID, "member")

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSMembershipEvent(t, "organization_membership.created", "event_01HZDEL1", membershipID, workosOrgID, workosUserID, time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC), "member"),
			newWorkOSMembershipEvent(t, "organization_membership.deleted", "event_01HZDEL2", membershipID, workosOrgID, workosUserID, time.Date(2026, 5, 6, 13, 0, 0, 0, time.UTC)),
		},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZDEL2", res.LastEventID)

	active, err := orgrepo.New(conn).HasOrganizationUserRelationship(ctx, orgrepo.HasOrganizationUserRelationshipParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
	require.False(t, active)

	relationship, err := orgrepo.New(conn).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
	require.True(t, relationship.Deleted)
	require.Equal(t, "event_01HZDEL2", relationship.WorkosLastEventID.String)

	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.True(t, assignments[0].DeletedAt.Valid)
	require.Equal(t, "event_01HZDEL2", assignments[0].WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_MembershipRejoinReusesTombstone(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_membership_rejoin")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_mem_rejoin"
	const workosOrgID = "org_01HZMEMREJOIN"
	const userID = "user_mem_rejoin"
	const workosUserID = "user_01HZMEMREJOIN"
	const firstMembershipID = "mem_01HZREJOIN1"
	const secondMembershipID = "mem_01HZREJOIN2"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedWorkOSUser(t, ctx, conn, userID, workosUserID)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSMembershipEvent(t, "organization_membership.created", "event_01HZREJOIN1", firstMembershipID, workosOrgID, workosUserID, time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)),
			newWorkOSMembershipEvent(t, "organization_membership.deleted", "event_01HZREJOIN2", firstMembershipID, workosOrgID, workosUserID, time.Date(2026, 5, 6, 13, 0, 0, 0, time.UTC)),
			newWorkOSMembershipEvent(t, "organization_membership.created", "event_01HZREJOIN3", secondMembershipID, workosOrgID, workosUserID, time.Date(2026, 5, 6, 14, 0, 0, 0, time.UTC)),
		},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZREJOIN3", res.LastEventID)

	relationship, err := orgrepo.New(conn).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
	require.False(t, relationship.Deleted)
	require.Equal(t, secondMembershipID, relationship.WorkosMembershipID.String)
	require.Equal(t, "event_01HZREJOIN3", relationship.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_MembershipUnknownOrganizationSkips(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_membership_unknown_org")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZMEMUNKORG"

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSMembershipEvent(t, "organization_membership.created", "event_01HZMEMUNKORG", "mem_01HZUNKNOWNORG", workosOrgID, "user_01HZUNKNOWNORG", time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC), "member"),
		},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZMEMUNKORG", res.LastEventID)

	cursor, err := workosrepo.New(conn).GetOrganizationSyncLastEventID(ctx, workosOrgID)
	require.NoError(t, err)
	require.Equal(t, "event_01HZMEMUNKORG", cursor)
}

func TestProcessWorkOSOrganizationEvents_MembershipMultipleRolesCreatesMultipleAssignments(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_membership_multi_role")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_multi_role"
	const workosOrgID = "org_01HZMULTIROLE"
	const userID = "user_multi_role"
	const workosUserID = "user_01HZMULTIROLE"

	updatedAt := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedWorkOSUser(t, ctx, conn, userID, workosUserID)
	adminRole := seedOrganizationRole(t, ctx, conn, organizationID, "admin")
	builderRole := seedOrganizationRole(t, ctx, conn, organizationID, "builder")
	viewerRole := seedOrganizationRole(t, ctx, conn, organizationID, "viewer")

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSMembershipEvent(t, "organization_membership.created", "event_01HZMULTI1", "mem_01HZMULTI", workosOrgID, workosUserID, updatedAt, "admin", "builder", "viewer"),
		},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZMULTI1", res.LastEventID)

	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 3)

	assignedURNs := make([]string, len(assignments))
	for i, a := range assignments {
		assignedURNs[i] = a.RoleUrn
		require.True(t, a.UserID.Valid, "user_id should be populated for known user")
		require.Equal(t, userID, a.UserID.String)
	}

	expectedURNs := []string{
		fmt.Sprintf("role:organization:%s", adminRole.ID.String()),
		fmt.Sprintf("role:organization:%s", builderRole.ID.String()),
		fmt.Sprintf("role:organization:%s", viewerRole.ID.String()),
	}
	require.ElementsMatch(t, expectedURNs, assignedURNs)
}

func TestProcessWorkOSOrganizationEvents_MembershipMultipleRolesUnknownUserOptimistic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_membership_multi_role_unknown")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_multi_role_unk"
	const workosOrgID = "org_01HZMULTIROLEUNK"
	const workosUserID = "user_01HZMULTIROLEUNK"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	editorRole := seedOrganizationRole(t, ctx, conn, organizationID, "editor")
	reviewerRole := seedOrganizationRole(t, ctx, conn, organizationID, "reviewer")

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSMembershipEvent(t, "organization_membership.created", "event_01HZMULTIUNK", "mem_01HZMULTIUNK", workosOrgID, workosUserID, time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC), "editor", "reviewer"),
		},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZMULTIUNK", res.LastEventID)

	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 2)

	assignedURNs := make([]string, len(assignments))
	for i, a := range assignments {
		assignedURNs[i] = a.RoleUrn
		require.False(t, a.UserID.Valid, "user_id should be empty for unknown user")
	}

	expectedURNs := []string{
		fmt.Sprintf("role:organization:%s", editorRole.ID.String()),
		fmt.Sprintf("role:organization:%s", reviewerRole.ID.String()),
	}
	require.ElementsMatch(t, expectedURNs, assignedURNs)
}

// ---------------------------------------------------------------------------
// SSO connection events
// ---------------------------------------------------------------------------

func newWorkOSConnectionEvent(t *testing.T, eventType, eventID, workosOrgID string) events.Event {
	t.Helper()
	data, err := json.Marshal(map[string]string{
		"id":              "conn_01HZ" + eventID,
		"object":          "connection",
		"organization_id": workosOrgID,
	})
	require.NoError(t, err)
	return events.Event{ID: eventID, Event: eventType, CreatedAt: time.Now(), Data: data}
}

func newWorkOSDSyncEvent(t *testing.T, eventType, eventID, workosOrgID string) events.Event {
	t.Helper()
	data, err := json.Marshal(map[string]string{
		"id":              "dir_01HZ" + eventID,
		"object":          "directory",
		"organization_id": workosOrgID,
	})
	require.NoError(t, err)
	return events.Event{ID: eventID, Event: eventType, CreatedAt: time.Now(), Data: data}
}

func TestProcessWorkOSOrganizationEvents_ConnectionActivatedSetsSSOEnabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_conn_activated")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_sso_activated"
	const workosOrgID = "org_01HZSSOACT"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{newWorkOSConnectionEvent(t, "connection.activated", "event_01HZSSOACT", workosOrgID)},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZSSOACT", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.True(t, row.SsoEnabled.Valid)
	require.True(t, row.SsoEnabled.Bool)
}

func TestProcessWorkOSOrganizationEvents_ConnectionDeactivatedClearsSSOEnabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_conn_deactivated")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_sso_deactivated"
	const workosOrgID = "org_01HZSSODEACT"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	// First activate, then deactivate.
	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSConnectionEvent(t, "connection.activated", "event_01HZSSO1", workosOrgID),
			newWorkOSConnectionEvent(t, "connection.deactivated", "event_01HZSSO2", workosOrgID),
		},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZSSO2", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.True(t, row.SsoEnabled.Valid)
	require.False(t, row.SsoEnabled.Bool)
}

func TestProcessWorkOSOrganizationEvents_ConnectionDeletedClearsSSOEnabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_conn_deleted")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_sso_deleted"
	const workosOrgID = "org_01HZSSODEL"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSConnectionEvent(t, "connection.activated", "event_01HZSSO3", workosOrgID),
			newWorkOSConnectionEvent(t, "connection.deleted", "event_01HZSSO4", workosOrgID),
		},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZSSO4", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.True(t, row.SsoEnabled.Valid)
	require.False(t, row.SsoEnabled.Bool)
}

func TestProcessWorkOSOrganizationEvents_ConnectionEventEmptyOrgIDSkips(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_conn_empty_org")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZSSONULLORG"

	// Connection event with empty organization_id — should be skipped gracefully.
	data, err := json.Marshal(map[string]string{
		"id":              "conn_01HZEMPTY",
		"object":          "connection",
		"organization_id": "",
	})
	require.NoError(t, err)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{{ID: "event_01HZSSONULL", Event: "connection.activated", CreatedAt: time.Now(), Data: data}},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZSSONULL", res.LastEventID)
}

func TestProcessWorkOSOrganizationEvents_ConnectionActivatedIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_conn_idempotent")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_sso_idempotent"
	const workosOrgID = "org_01HZSSOIDEM"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	// Two consecutive activations — second should be a no-op.
	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSConnectionEvent(t, "connection.activated", "event_01HZSSO5", workosOrgID),
			newWorkOSConnectionEvent(t, "connection.activated", "event_01HZSSO6", workosOrgID),
		},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZSSO6", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.True(t, row.SsoEnabled.Valid)
	require.True(t, row.SsoEnabled.Bool)
}

// ---------------------------------------------------------------------------
// SCIM / Directory Sync events
// ---------------------------------------------------------------------------

func TestProcessWorkOSOrganizationEvents_DSyncActivatedSetsSCIMEnabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_dsync_activated")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_scim_activated"
	const workosOrgID = "org_01HZSCIMACT"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{newWorkOSDSyncEvent(t, "dsync.activated", "event_01HZSCIMACT", workosOrgID)},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZSCIMACT", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.True(t, row.ScimEnabled.Valid)
	require.True(t, row.ScimEnabled.Bool)
}

func TestProcessWorkOSOrganizationEvents_DSyncDeletedClearsSCIMEnabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_dsync_deleted")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_scim_deleted"
	const workosOrgID = "org_01HZSCIMDEL"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSDSyncEvent(t, "dsync.activated", "event_01HZSCIM3", workosOrgID),
			newWorkOSDSyncEvent(t, "dsync.deleted", "event_01HZSCIM4", workosOrgID),
		},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZSCIM4", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.True(t, row.ScimEnabled.Valid)
	require.False(t, row.ScimEnabled.Bool)
}

func TestProcessWorkOSOrganizationEvents_DSyncEventEmptyOrgIDSkips(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_dsync_empty_org")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZSCIMNULLORG"

	data, err := json.Marshal(map[string]string{
		"id":              "dir_01HZEMPTY",
		"object":          "directory",
		"organization_id": "",
	})
	require.NoError(t, err)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{{ID: "event_01HZSCIMNULL", Event: "dsync.activated", CreatedAt: time.Now(), Data: data}},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZSCIMNULL", res.LastEventID)
}

func TestProcessWorkOSOrganizationEvents_DSyncActivatedIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_dsync_idempotent")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_scim_idempotent"
	const workosOrgID = "org_01HZSCIMIDEMP"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSDSyncEvent(t, "dsync.activated", "event_01HZSCIM5", workosOrgID),
			newWorkOSDSyncEvent(t, "dsync.activated", "event_01HZSCIM6", workosOrgID),
		},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZSCIM6", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.True(t, row.ScimEnabled.Valid)
	require.True(t, row.ScimEnabled.Bool)
}

// ---------------------------------------------------------------------------
// Combined SSO + SCIM lifecycle
// ---------------------------------------------------------------------------

func TestProcessWorkOSOrganizationEvents_SSOAndSCIMFullLifecycle(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_sso_scim_lifecycle")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_sso_scim_full"
	const workosOrgID = "org_01HZLIFECYCLE"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	// Full lifecycle: activate SSO → activate SCIM → deactivate SSO → delete SCIM
	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSConnectionEvent(t, "connection.activated", "event_01HZL1", workosOrgID),
			newWorkOSDSyncEvent(t, "dsync.activated", "event_01HZL2", workosOrgID),
			newWorkOSConnectionEvent(t, "connection.deactivated", "event_01HZL3", workosOrgID),
			newWorkOSDSyncEvent(t, "dsync.deleted", "event_01HZL4", workosOrgID),
		},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZL4", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.True(t, row.SsoEnabled.Valid)
	require.False(t, row.SsoEnabled.Bool, "SSO should be disabled after connection.deactivated")
	require.True(t, row.ScimEnabled.Valid)
	require.False(t, row.ScimEnabled.Bool, "SCIM should be disabled after dsync.deleted")
}

func TestProcessWorkOSOrganizationEvents_StaleConnectionEventSkipped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_conn_stale")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_sso_stale"
	const workosOrgID = "org_01HZSSOSTALE"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	// Deliver activation with a high event ID, then a stale deactivation
	// with a lower event ID (simulating out-of-order delivery).
	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSConnectionEvent(t, "connection.activated", "event_01HZSSOSTALE2", workosOrgID),
			newWorkOSConnectionEvent(t, "connection.deactivated", "event_01HZSSOSTALE1", workosOrgID),
		},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	// LastEventID tracks the last event processed by the loop (including
	// skipped stale events), so it advances to the final event in the batch.
	require.Equal(t, "event_01HZSSOSTALE1", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.True(t, row.SsoEnabled.Valid)
	require.True(t, row.SsoEnabled.Bool, "SSO should remain enabled — stale deactivation must be skipped")
	// The DB cursor should reflect the newer event that was actually applied,
	// not the stale event that was skipped.
	require.Equal(t, "event_01HZSSOSTALE2", row.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_StaleDSyncEventSkipped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_dsync_stale")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_scim_stale"
	const workosOrgID = "org_01HZSCIMSTALE"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	// Deliver activation with a high event ID, then a stale deletion
	// with a lower event ID (simulating out-of-order delivery).
	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			newWorkOSDSyncEvent(t, "dsync.activated", "event_01HZSCIMSTALE2", workosOrgID),
			newWorkOSDSyncEvent(t, "dsync.deleted", "event_01HZSCIMSTALE1", workosOrgID),
		},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZSCIMSTALE1", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.True(t, row.ScimEnabled.Valid)
	require.True(t, row.ScimEnabled.Bool, "SCIM should remain enabled — stale deletion must be skipped")
	require.Equal(t, "event_01HZSCIMSTALE2", row.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_ConnectionEventUnknownOrgNoError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_conn_unknown_org")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZSSOUNKORG"

	// connection.activated for a workos org that has no matching Gram org — UPDATE
	// matches 0 rows, which is fine (no error).
	stub := newWorkOSClientWithEvents([][]events.Event{
		{newWorkOSConnectionEvent(t, "connection.activated", "event_01HZSSOUNK", workosOrgID)},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZSSOUNK", res.LastEventID)
}

func TestProcessWorkOSOrganizationEvents_DSyncEventUnknownOrgNoError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_dsync_unknown_org")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZSCIMUNKORG"

	stub := newWorkOSClientWithEvents([][]events.Event{
		{newWorkOSDSyncEvent(t, "dsync.activated", "event_01HZSCIMUNK", workosOrgID)},
	})
	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZSCIMUNK", res.LastEventID)
}

func TestProcessWorkOSOrganizationEvents_OrganizationRoleSkippedForUnknownOrg(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_role_unknown_org")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZUNKNOWN"

	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{
				ID:        "event_01HZUR1",
				Event:     "organization_role.created",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"role_01","object":"role","organization_id":"` + workosOrgID +
					`","slug":"phantom","name":"Phantom","type":"OrganizationRole",` +
					`"created_at":"2026-05-06T10:00:00Z","updated_at":"2026-05-06T10:00:00Z"}`),
			},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	// Cursor still advances — we don't replay events for orgs that don't yet
	// exist locally, the reconcile/backfill workflow will catch them up.
	require.Equal(t, "event_01HZUR1", res.LastEventID)

	_, err = accessrepo.New(conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
		OrganizationID: "nope",
		WorkosSlug:     "phantom",
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
