package activities_test

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestProcessWorkOSOrganizationEvents_SkipsUnlinkedOrgWithoutExternalID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_org_events_unlinked_no_external_id")
	logger := testenv.NewLogger(t)

	const workosOrgID = "org_01HZTESTBAD"

	// First event has neither a Gram-side mapping nor an external_id — must
	// not stall the stream. Second event in the same page carries the
	// external_id and should be applied normally.
	stub := newWorkOSClientWithEvents([][]events.Event{
		{
			{ID: "event_01HZBAD", Event: "organization.updated", CreatedAt: time.Now(), Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Pending","updated_at":"2026-05-06T11:00:00Z"}`)},
			{ID: "event_01HZGOOD", Event: "organization.updated", CreatedAt: time.Now(), Data: []byte(`{"id":"` + workosOrgID + `","object":"organization","name":"Resolved","external_id":"sb_resolved","updated_at":"2026-05-06T12:00:00Z"}`)},
		},
	})

	activity := activities.NewProcessWorkOSOrganizationEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSOrganizationEventsParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)
	require.Equal(t, "event_01HZGOOD", res.LastEventID)

	row, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, "sb_resolved", row.ID)
	require.Equal(t, "Resolved", row.Name)

	cursor, err := workosrepo.New(conn).GetOrganizationSyncLastEventID(ctx, workosOrgID)
	require.NoError(t, err)
	require.Equal(t, "event_01HZGOOD", cursor)
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
	_, err := orgrepo.New(conn).UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:                externalID,
		Name:              "Live",
		Slug:              "live",
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

	_, err := orgrepo.New(conn).UpsertOrganizationMetadataFromWorkOS(ctx, orgrepo.UpsertOrganizationMetadataFromWorkOSParams{
		ID:                organizationID,
		Name:              "Test Org",
		Slug:              organizationID,
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
