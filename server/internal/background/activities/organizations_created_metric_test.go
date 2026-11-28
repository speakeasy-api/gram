package activities_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	"github.com/workos/workos-go/v6/pkg/events"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgmetrics"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgmetrics/orgmetricstest"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
)

// orgsCreatedLockTimeout bounds how long event processing waits on the held
// sync cursor row. It only needs to outlast scheduling noise, since the row
// stays locked until the test releases it.
const orgsCreatedLockTimeout = 500 * time.Millisecond

func orgsCreatedEventPayload(workosOrgID, name, externalID string) []byte {
	return []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"` + name + `","external_id":"` + externalID +
		`","updated_at":"2026-05-06T12:00:00Z"}`)
}

func TestProcessWorkOSOrganizationEvents_OrganizationsCreatedMetric_CreatedEventForUnknownOrganizationRecordsWorkOSSync(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "workos_org_events_orgs_created_unknown")

	const workosOrgID = "org_01HZORGSCREATEDNEW"
	const externalID = "sb_orgs_created_new"

	stub := newWorkOSClientWithEvents([][]events.Event{
		{{ID: "event_01HZOCN1", Event: "organization.created", CreatedAt: time.Now(), Data: orgsCreatedEventPayload(workosOrgID, "Created New", externalID)}},
	})

	created, reader := orgmetricstest.New(t)
	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), conn, stub, cache.NoopCache, nil)
	activity.SetOrganizationsCreated(created)

	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, externalID, row.ID)
	reader.RequireOnly(t, orgmetrics.CreationSourceWorkOSSync, 1)
}

// An organization.updated event for an organization Speakeasy has not seen
// inserts it too, so it counts as a WorkOS sync creation.
func TestProcessWorkOSOrganizationEvents_OrganizationsCreatedMetric_UpdatedEventForUnknownOrganizationRecordsWorkOSSync(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "workos_org_events_orgs_created_update_unknown")

	const workosOrgID = "org_01HZORGSCREATEDUPD"

	stub := newWorkOSClientWithEvents([][]events.Event{
		{{ID: "event_01HZOCU1", Event: "organization.updated", CreatedAt: time.Now(), Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Updated New","updated_at":"2026-05-06T12:00:00Z"}`)}},
	})

	created, reader := orgmetricstest.New(t)
	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), conn, stub, cache.NoopCache, nil)
	activity.SetOrganizationsCreated(created)

	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	_, err = orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	reader.RequireOnly(t, orgmetrics.CreationSourceWorkOSSync, 1)
}

func TestProcessWorkOSOrganizationEvents_OrganizationsCreatedMetric_ReplayAndLaterUpdateRecordNothingMore(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "workos_org_events_orgs_created_replay")

	const workosOrgID = "org_01HZORGSCREATEDREPLAY"
	const externalID = "sb_orgs_created_replay"

	createdEvent := events.Event{ID: "event_01HZOCR1", Event: "organization.created", CreatedAt: time.Now(), Data: orgsCreatedEventPayload(workosOrgID, "Replay", externalID)}
	stub := newWorkOSClientWithEvents([][]events.Event{
		{createdEvent},
		{
			createdEvent,
			{ID: "event_01HZOCR2", Event: "organization.updated", CreatedAt: time.Now(), Data: orgsCreatedEventPayload(workosOrgID, "Replay Renamed", externalID)},
		},
	})

	created, reader := orgmetricstest.New(t)
	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), conn, stub, cache.NoopCache, nil)
	activity.SetOrganizationsCreated(created)

	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	reader.RequireOnly(t, orgmetrics.CreationSourceWorkOSSync, 1)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZOCR2", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationMetadata(ctx, externalID)
	require.NoError(t, err)
	require.Equal(t, "Replay Renamed", row.Name)
	reader.RequireOnly(t, orgmetrics.CreationSourceWorkOSSync, 1)
}

// A signup inserts the organization before WorkOS reports it, and the event
// finds that row through its external_id.
func TestProcessWorkOSOrganizationEvents_OrganizationsCreatedMetric_CreatedEventForSignupByExternalIDRecordsNothing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "workos_org_events_orgs_created_external_id")

	const workosOrgID = "org_01HZORGSCREATEDEXT"
	const externalID = "sb_orgs_created_ext"

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:             externalID,
		Name:           "Signup",
		Slug:           "signup",
		WorkosID:       conv.ToPGTextEmpty(""),
		Whitelisted:    pgtype.Bool{Bool: false, Valid: false},
		CreationSource: conv.ToPGText(orgprovision.SourceSignup),
		DefaultHost:    conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	stub := newWorkOSClientWithEvents([][]events.Event{
		{{ID: "event_01HZOCE1", Event: "organization.created", CreatedAt: time.Now(), Data: orgsCreatedEventPayload(workosOrgID, "Signup", externalID)}},
	})

	created, reader := orgmetricstest.New(t)
	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), conn, stub, cache.NoopCache, nil)
	activity.SetOrganizationsCreated(created)

	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, externalID, row.ID)
	require.Empty(t, reader.Counts(t))
}

// A signup inserts the organization already linked to its WorkOS
// organization, and the event finds that row through workos_id.
func TestProcessWorkOSOrganizationEvents_OrganizationsCreatedMetric_CreatedEventForSignupByWorkOSIDRecordsNothing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "workos_org_events_orgs_created_workos_id")

	const workosOrgID = "org_01HZORGSCREATEDWID"
	const organizationID = "sb_orgs_created_wid"

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:             organizationID,
		Name:           "Linked Signup",
		Slug:           "linked-signup",
		WorkosID:       conv.ToPGText(workosOrgID),
		Whitelisted:    pgtype.Bool{Bool: false, Valid: false},
		CreationSource: conv.ToPGText(orgprovision.SourceSignup),
		DefaultHost:    conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	// The payload carries no external_id, so only the workos_id link matches.
	stub := newWorkOSClientWithEvents([][]events.Event{
		{{ID: "event_01HZOCW1", Event: "organization.created", CreatedAt: time.Now(), Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Linked Signup","updated_at":"2026-05-06T12:00:00Z"}`)}},
	})

	created, reader := orgmetricstest.New(t)
	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), conn, stub, cache.NoopCache, nil)
	activity.SetOrganizationsCreated(created)

	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, organizationID, row.ID)
	require.Empty(t, reader.Counts(t))
}

// The event inserts the organization, then waits on the sync cursor row a
// test transaction holds until lock_timeout aborts it. The rolled back insert
// records nothing; the retry after the row is released records once.
func TestProcessWorkOSOrganizationEvents_OrganizationsCreatedMetric_RolledBackEventRecordsNothingAndRetryRecordsOnce(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newOrgEventsTestConn(t, "workos_org_events_orgs_created_rollback")

	const workosOrgID = "org_01HZORGSCREATEDROLLBACK"
	const externalID = "sb_orgs_created_rollback"

	_, err := workosrepo.New(conn).SetOrganizationSyncLastEventID(ctx, workosrepo.SetOrganizationSyncLastEventIDParams{
		WorkosOrganizationID: workosOrgID,
		LastEventID:          "event_01HZOCB0",
	})
	require.NoError(t, err)

	createdEvent := events.Event{ID: "event_01HZOCB1", Event: "organization.created", CreatedAt: time.Now(), Data: orgsCreatedEventPayload(workosOrgID, "Rollback", externalID)}
	stub := newWorkOSClientWithEvents([][]events.Event{{createdEvent}, {createdEvent}})

	created, reader := orgmetricstest.New(t)
	lockTimeoutConn := testenv.NewLockTimeoutPool(t, conn, orgsCreatedLockTimeout)
	activity := activities.NewProcessWorkOSOrganizationEvents(testenv.NewLogger(t), lockTimeoutConn, stub, cache.NoopCache, nil)
	activity.SetOrganizationsCreated(created)

	holder := testenv.BeginTx(t, ctx, conn)
	_, err = workosrepo.New(holder).SetOrganizationSyncLastEventID(ctx, workosrepo.SetOrganizationSyncLastEventIDParams{
		WorkosOrganizationID: workosOrgID,
		LastEventID:          "event_01HZOCB0",
	})
	require.NoError(t, err)

	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	testenv.RequireLockNotAvailable(t, err)

	_, err = orgrepo.New(conn).GetOrganizationMetadata(ctx, externalID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Empty(t, reader.Counts(t))

	require.NoError(t, holder.Rollback(ctx))

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZOCB1", res.LastEventID)

	_, err = orgrepo.New(conn).GetOrganizationMetadata(ctx, externalID)
	require.NoError(t, err)
	reader.RequireOnly(t, orgmetrics.CreationSourceWorkOSSync, 1)
}
