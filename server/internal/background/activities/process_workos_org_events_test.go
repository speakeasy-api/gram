package activities_test

import (
	"context"
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
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
	// external_id maps to the Speakeasy organization id; the upsert relies on
	// it for orgs not yet linked via workos_id.
	return []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Test","external_id":"sb_` + workosOrgID + `","updated_at":"2026-05-06T12:00:00Z"}`)
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

func TestProcessWorkOSOrganizationEvents_OrganizationCreatedAndUpdated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_create")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZCREATE"
	const externalID = "sb_01HZCREATE"

	created := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 5, 6, 11, 0, 0, 0, time.UTC)

	stub := &stubWorkOSEventsClient{
		pages: [][]events.Event{{
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
		}},
	}

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, externalID, row.ID)
	require.Equal(t, "Acme Inc", row.Name)
	require.True(t, row.WorkosLastEventID.Valid)
	require.Equal(t, "event_01HZB", row.WorkosLastEventID.String)
	require.True(t, row.WorkosUpdatedAt.Valid)
	require.True(t, row.WorkosUpdatedAt.Time.Equal(updated))
	require.False(t, row.DisabledAt.Valid)
}

func TestProcessWorkOSOrganizationEvents_OrganizationUpdateSkippedWhenStale(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_org_stale")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZSTALE"
	const externalID = "sb_01HZSTALE"

	stub := &stubWorkOSEventsClient{
		pages: [][]events.Event{{
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
		}},
	}

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
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

	_, err := orgrepo.New(conn).UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:                externalID,
		Name:              "Doomed",
		Slug:              "doomed",
		WorkosID:          conv.ToPGText(workosOrgID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(time.Now()),
		WorkosLastEventID: conv.ToPGText("event_00SEED"),
	})
	require.NoError(t, err)

	stub := &stubWorkOSEventsClient{
		pages: [][]events.Event{{
			{
				ID:        "event_01HZDEL",
				Event:     "organization.deleted",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Doomed","external_id":"` + externalID +
					`","updated_at":"2026-05-06T12:00:00Z"}`),
			},
		}},
	}

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.True(t, row.DisabledAt.Valid)
	require.Equal(t, "event_01HZDEL", row.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_GlobalRoleUpsertAndDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_global_role")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZGLOBALROLE"
	const slug = "platform-admin"

	stub := &stubWorkOSEventsClient{
		pages: [][]events.Event{{
			{
				ID:        "event_01HZG1",
				Event:     "organization_role.created",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"role_01","object":"role","organization_id":"","slug":"` + slug + `","name":"Platform Admin",` +
					`"description":"env-level admin","type":"EnvironmentRole",` +
					`"created_at":"2026-05-06T10:00:00Z","updated_at":"2026-05-06T10:00:00Z"}`),
			},
			{
				ID:        "event_01HZG2",
				Event:     "organization_role.deleted",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"role_01","object":"role","organization_id":"","slug":"` + slug + `","name":"Platform Admin",` +
					`"type":"EnvironmentRole","created_at":"2026-05-06T10:00:00Z","updated_at":"2026-05-06T11:00:00Z",` +
					`"deleted_at":"2026-05-06T11:00:00Z"}`),
			},
		}},
	}

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	_, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
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

	_, err := orgrepo.New(conn).UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:                externalID,
		Name:              "Acme",
		Slug:              "acme",
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

	stub := &stubWorkOSEventsClient{
		pages: [][]events.Event{{
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
		}},
	}

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
	_, err := orgrepo.New(conn).UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:                externalID,
		Name:              "Live",
		Slug:              "live",
		WorkosID:          conv.ToPGText(workosOrgID),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(time.Now()),
		WorkosLastEventID: conv.ToPGText("event_99FRESH"),
	})
	require.NoError(t, err)

	stub := &stubWorkOSEventsClient{
		pages: [][]events.Event{{
			{
				ID:        "event_01HZSTALEDEL",
				Event:     "organization.deleted",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Live","external_id":"` + externalID +
					`","updated_at":"2026-05-06T11:00:00Z"}`),
			},
		}},
	}

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)
	_, err = activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.False(t, row.DisabledAt.Valid, "stale delete event must not disable a fresher org row")
	require.Equal(t, "event_99FRESH", row.WorkosLastEventID.String)
}

func TestProcessWorkOSOrganizationEvents_OrganizationRoleSkippedForUnknownOrg(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_role_unknown_org")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZUNKNOWN"

	stub := &stubWorkOSEventsClient{
		pages: [][]events.Event{{
			{
				ID:        "event_01HZUR1",
				Event:     "organization_role.created",
				CreatedAt: time.Now(),
				Data: []byte(`{"id":"role_01","object":"role","organization_id":"` + workosOrgID +
					`","slug":"phantom","name":"Phantom","type":"OrganizationRole",` +
					`"created_at":"2026-05-06T10:00:00Z","updated_at":"2026-05-06T10:00:00Z"}`),
			},
		}},
	}

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
