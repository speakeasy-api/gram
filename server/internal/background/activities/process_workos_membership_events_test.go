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
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func newMembershipEventsTestConn(t *testing.T, name string) *pgxpool.Pool {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	return conn
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

	err = usersrepo.New(conn).SetUserWorkosID(ctx, usersrepo.SetUserWorkosIDParams{
		WorkosID: conv.ToPGText(workosUserID),
		ID:       userID,
	})
	require.NoError(t, err)
}

func seedOrganizationRole(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, slug string) accessrepo.OrganizationRole {
	t.Helper()

	eventTime := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	err := accessrepo.New(conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
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

func seedGlobalRole(t *testing.T, ctx context.Context, conn *pgxpool.Pool, slug string) accessrepo.GlobalRole {
	t.Helper()

	eventTime := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	err := accessrepo.New(conn).UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{
		WorkosSlug:        slug,
		WorkosName:        slug,
		WorkosDescription: conv.ToPGText(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(eventTime),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(eventTime),
		WorkosLastEventID: conv.ToPGText("event_00SEED"),
	})
	require.NoError(t, err)

	role, err := accessrepo.New(conn).GetGlobalRoleBySlug(ctx, slug)
	require.NoError(t, err)
	return role
}

func TestProcessWorkOSMembershipEvents_CursorResumesFromDB(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newMembershipEventsTestConn(t, "workos_membership_events_cursor")
	logger := testenv.NewLogger(t)

	const storedCursor = "event_01HZCURSOR"
	_, err := workosrepo.New(conn).SetUserSyncLastEventID(ctx, storedCursor)
	require.NoError(t, err)

	stub := &stubWorkOSEventsClient{pages: [][]events.Event{nil}}
	activity := activities.NewProcessWorkOSMembershipEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSMembershipEventsParams{})
	require.NoError(t, err)
	require.Equal(t, storedCursor, res.SinceEventID)
	require.Len(t, stub.calls, 1)
	require.Equal(t, storedCursor, stub.calls[0].After)
	require.Empty(t, stub.calls[0].OrganizationId)
	require.ElementsMatch(t, []string{
		"organization_membership.created",
		"organization_membership.deleted",
		"organization_membership.updated",
	}, stub.calls[0].Events)
}

func TestProcessWorkOSMembershipEvents_KnownUserSyncsMembershipAndRoles(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newMembershipEventsTestConn(t, "workos_membership_events_known_user")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_mem_known"
	const workosOrgID = "org_01HZMEMKNOWN"
	const userID = "user_mem_known"
	const workosUserID = "user_01HZMEMKNOWN"

	updatedAt := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedWorkOSUser(t, ctx, conn, userID, workosUserID)
	organizationRole := seedOrganizationRole(t, ctx, conn, organizationID, "member")
	globalRole := seedGlobalRole(t, ctx, conn, "platform-admin")

	stub := &stubWorkOSEventsClient{
		pages: [][]events.Event{{
			newWorkOSMembershipEvent(t, "organization_membership.created", "event_01HZMEM1", "mem_01HZKNOWN", workosOrgID, workosUserID, updatedAt, "member", "platform-admin"),
		}},
	}
	activity := activities.NewProcessWorkOSMembershipEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSMembershipEventsParams{})
	require.NoError(t, err)
	require.Equal(t, "event_01HZMEM1", res.LastEventID)

	relationship, err := orgrepo.New(conn).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         userID,
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
	require.Len(t, assignments, 2)
	require.ElementsMatch(t, []string{
		fmt.Sprintf("role:global:%s", globalRole.ID.String()),
		fmt.Sprintf("role:organization:%s", organizationRole.ID.String()),
	}, []string{assignments[0].RoleUrn, assignments[1].RoleUrn})

	cursor, err := workosrepo.New(conn).GetUserSyncLastEventID(ctx)
	require.NoError(t, err)
	require.Equal(t, "event_01HZMEM1", cursor)
}

func TestProcessWorkOSMembershipEvents_UnknownUserSyncsRolesAndAdvancesCursor(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newMembershipEventsTestConn(t, "workos_membership_events_unknown_user")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_mem_unknown_user"
	const workosOrgID = "org_01HZMEMUNKNOWNUSER"
	const workosUserID = "user_01HZMEMUNKNOWN"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	memberRole := seedOrganizationRole(t, ctx, conn, organizationID, "member")

	stub := &stubWorkOSEventsClient{
		pages: [][]events.Event{{
			newWorkOSMembershipEvent(t, "organization_membership.created", "event_01HZMEMUNK", "mem_01HZUNKNOWN", workosOrgID, workosUserID, time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC), "member"),
		}},
	}
	activity := activities.NewProcessWorkOSMembershipEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSMembershipEventsParams{})
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

	cursor, err := workosrepo.New(conn).GetUserSyncLastEventID(ctx)
	require.NoError(t, err)
	require.Equal(t, "event_01HZMEMUNK", cursor)
}

func TestProcessWorkOSMembershipEvents_StaleUpdateSkipped(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newMembershipEventsTestConn(t, "workos_membership_events_stale")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_mem_stale"
	const workosOrgID = "org_01HZMEMSTALE"
	const userID = "user_mem_stale"
	const workosUserID = "user_01HZMEMSTALE"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedWorkOSUser(t, ctx, conn, userID, workosUserID)
	memberRole := seedOrganizationRole(t, ctx, conn, organizationID, "member")
	seedOrganizationRole(t, ctx, conn, organizationID, "admin")

	freshTime := time.Date(2026, 5, 6, 13, 0, 0, 0, time.UTC)
	err := orgrepo.New(conn).UpsertOrganizationUserRelationshipFromWorkOS(ctx, orgrepo.UpsertOrganizationUserRelationshipFromWorkOSParams{
		OrganizationID:     organizationID,
		UserID:             userID,
		WorkosMembershipID: conv.ToPGText("mem_01HZSTALE"),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(freshTime),
		WorkosLastEventID:  conv.ToPGText("event_99FRESH"),
	})
	require.NoError(t, err)
	err = orgrepo.New(conn).SyncUserOrganizationRoleAssignments(ctx, orgrepo.SyncUserOrganizationRoleAssignmentsParams{
		OrganizationID:     organizationID,
		WorkosUserID:       workosUserID,
		WorkosRoleSlugs:    []string{"member"},
		UserID:             conv.ToPGText(userID),
		WorkosMembershipID: conv.ToPGText("mem_01HZSTALE"),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(freshTime),
		WorkosLastEventID:  conv.ToPGText("event_99FRESH"),
	})
	require.NoError(t, err)

	stub := &stubWorkOSEventsClient{
		pages: [][]events.Event{{
			newWorkOSMembershipEvent(t, "organization_membership.updated", "event_01HZSTALE", "mem_01HZSTALE", workosOrgID, workosUserID, time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC), "admin"),
		}},
	}
	activity := activities.NewProcessWorkOSMembershipEvents(logger, conn, stub)

	_, err = activity.Do(ctx, activities.ProcessWorkOSMembershipEventsParams{})
	require.NoError(t, err)

	relationship, err := orgrepo.New(conn).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         userID,
	})
	require.NoError(t, err)
	require.Equal(t, "event_99FRESH", relationship.WorkosLastEventID.String)

	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, fmt.Sprintf("role:organization:%s", memberRole.ID.String()), assignments[0].RoleUrn)
}

func TestProcessWorkOSMembershipEvents_DeleteSoftDeletesRelationshipAndAssignments(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newMembershipEventsTestConn(t, "workos_membership_events_delete")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_mem_delete"
	const workosOrgID = "org_01HZMEMDELETE"
	const userID = "user_mem_delete"
	const workosUserID = "user_01HZMEMDELETE"
	const membershipID = "mem_01HZDELETE"

	seedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedWorkOSUser(t, ctx, conn, userID, workosUserID)
	seedOrganizationRole(t, ctx, conn, organizationID, "member")

	stub := &stubWorkOSEventsClient{
		pages: [][]events.Event{{
			newWorkOSMembershipEvent(t, "organization_membership.created", "event_01HZDEL1", membershipID, workosOrgID, workosUserID, time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC), "member"),
			newWorkOSMembershipEvent(t, "organization_membership.deleted", "event_01HZDEL2", membershipID, workosOrgID, workosUserID, time.Date(2026, 5, 6, 13, 0, 0, 0, time.UTC)),
		}},
	}
	activity := activities.NewProcessWorkOSMembershipEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSMembershipEventsParams{})
	require.NoError(t, err)
	require.Equal(t, "event_01HZDEL2", res.LastEventID)

	active, err := orgrepo.New(conn).HasOrganizationUserRelationship(ctx, orgrepo.HasOrganizationUserRelationshipParams{
		OrganizationID: organizationID,
		UserID:         userID,
	})
	require.NoError(t, err)
	require.False(t, active)

	relationship, err := orgrepo.New(conn).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         userID,
	})
	require.NoError(t, err)
	require.True(t, relationship.Deleted)
	require.Equal(t, "event_01HZDEL2", relationship.WorkosLastEventID.String)

	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Empty(t, assignments)
}

func TestProcessWorkOSMembershipEvents_UnknownOrganizationSkips(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newMembershipEventsTestConn(t, "workos_membership_events_unknown_org")
	logger := testenv.NewLogger(t)

	stub := &stubWorkOSEventsClient{
		pages: [][]events.Event{{
			newWorkOSMembershipEvent(t, "organization_membership.created", "event_01HZMEMUNKORG", "mem_01HZUNKNOWNORG", "org_01HZUNKNOWN", "user_01HZUNKNOWNORG", time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC), "member"),
		}},
	}
	activity := activities.NewProcessWorkOSMembershipEvents(logger, conn, stub)

	res, err := activity.Do(ctx, activities.ProcessWorkOSMembershipEventsParams{})
	require.NoError(t, err)
	require.Equal(t, "event_01HZMEMUNKORG", res.LastEventID)

	cursor, err := workosrepo.New(conn).GetUserSyncLastEventID(ctx)
	require.NoError(t, err)
	require.Equal(t, "event_01HZMEMUNKORG", cursor)

	_, err = orgrepo.New(conn).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: "unknown",
		UserID:         "unknown",
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
