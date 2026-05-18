package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

func newBackfillTestConn(t *testing.T, name string) *pgxpool.Pool {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	return conn
}

func TestRunOrganizationBackfill_SkipsNoopOrganization(t *testing.T) {
	t.Parallel()

	rep := runOrganizationBackfill(
		context.Background(),
		testenv.NewLogger(t),
		nil,
		nil,
		options{
			phase:            phaseOrganizations,
			environment:      envLocal,
			databaseURL:      "",
			cloudSQLProxy:    false,
			cloudSQLPort:     0,
			cloudSQLDBName:   "gram",
			workosAPIKey:     "",
			workosEndpoint:   "",
			workosOrgIDs:     nil,
			limit:            0,
			dryRun:           false,
			autoApprove:      false,
			pauseAfterEach:   false,
			confirmProd:      "",
			breakpointBefore: false,
		},
		[]orgExpectation{{
			workosOrgID: "org_noop",
			gramOrgID:   "gram_noop",
			name:        "Noop",
			skipped:     false,
			roles:       nil,
			users:       nil,
			members:     nil,
			orgChanges: changeCounts{
				Create:    0,
				Update:    0,
				Noop:      1,
				Delete:    0,
				StaleSkip: 0,
			},
			roleChanges:       changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
			userChanges:       changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
			membershipChanges: changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
			assignmentChanges: changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
			changeDetails:     nil,
		}},
	)

	require.Equal(t, report{
		scanned:            1,
		skipped:            0,
		skippedNoop:        1,
		written:            0,
		validated:          0,
		failed:             0,
		validationFailures: 0,
		organizationRows:   changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		roleRows:           changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		userRows:           changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		membershipRows:     changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
		assignmentRows:     changeCounts{Create: 0, Update: 0, Noop: 0, Delete: 0, StaleSkip: 0},
	}, rep)
}

func TestBackfillWorkOSOrganization_CreatesUnlinkedOrganizationWithExternalID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newBackfillTestConn(t, "workos_backfill_create_org_external_id")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_from_workos_external_id"
	const workosOrgID = "org_01JBACKFILLCREATE"

	workosClient := newWorkOSSnapshotClient(t, ctx,
		workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Created Org",
			ExternalID: organizationID,
			CreatedAt:  "2026-05-07T11:00:00Z",
			UpdatedAt:  "2026-05-07T11:00:00Z",
		},
		nil,
		nil,
	)
	activity := NewBackfillWorkOSOrganization(logger, conn, workosClient)

	err := activity.Do(ctx, BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	org, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, organizationID, org.ID)
	require.Equal(t, "Backfill Created Org", org.Name)
	require.Equal(t, "backfill-created-org", org.Slug)
	require.Equal(t, workosOrgID, org.WorkosID.String)
	require.Empty(t, org.WorkosLastEventID.String)
}

func TestBackfillWorkOSOrganization_CreatesUniqueSlugOnNameCollision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newBackfillTestConn(t, "workos_backfill_create_org_slug_collision")
	logger := testenv.NewLogger(t)

	const existingOrganizationID = "gram_org_existing_tester"
	const organizationID = "gram_org_new_tester"
	const workosOrgID = "org_01JBACKFILLSLUGCOLLISION"

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          existingOrganizationID,
		Name:        "tester",
		Slug:        "tester",
		WorkosID:    conv.ToPGText("org_01JEXISTINGTESTER"),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	workosClient := newWorkOSSnapshotClient(t, ctx,
		workos.Organization{
			ID:         workosOrgID,
			Name:       "tester",
			ExternalID: organizationID,
			CreatedAt:  "2026-05-07T11:00:00Z",
			UpdatedAt:  "2026-05-07T11:00:00Z",
		},
		nil,
		nil,
	)
	activity := NewBackfillWorkOSOrganization(logger, conn, workosClient)

	err = activity.Do(ctx, BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	org, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, organizationID, org.ID)
	require.Equal(t, "tester", org.Name)
	require.NotEqual(t, "tester", org.Slug)
	require.Contains(t, org.Slug, "tester-")
	require.Len(t, org.Slug, len("tester-")+4)
}

func TestBackfillWorkOSOrganization_ExternalIDChangeDoesNotChangeOrganizationID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newBackfillTestConn(t, "workos_backfill_external_id_immutable")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_original_external_id"
	const changedExternalID = "gram_org_changed_external_id"
	const workosOrgID = "org_01JBACKFILLIMMUTABLE"

	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	workosClient := newWorkOSSnapshotClient(t, ctx,
		workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Immutable Org",
			ExternalID: changedExternalID,
			CreatedAt:  "2026-05-07T11:00:00Z",
			UpdatedAt:  "2026-05-07T11:00:00Z",
		},
		nil,
		nil,
	)
	activity := NewBackfillWorkOSOrganization(logger, conn, workosClient)

	err := activity.Do(ctx, BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	org, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, organizationID, org.ID)
	require.Equal(t, "Backfill Immutable Org", org.Name)

	_, err = orgrepo.New(conn).GetOrganizationMetadata(ctx, changedExternalID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestBackfillWorkOSOrganization_BackfillsUserAndSyncsSingleRoleAssignment(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newBackfillTestConn(t, "workos_backfill_user_single_role")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_backfill_user"
	const workosOrgID = "org_01JBACKFILLUSER"
	const workosUserID = "user_01JBACKFILLUSER"
	const gramUserID = "gram_user_01JBACKFILLUSER"
	const membershipID = "mem_01JBACKFILLUSER"
	const roleSlug = "org-support"

	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	workosClient := newWorkOSSnapshotClient(t, ctx,
		workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill User",
			ExternalID: "",
			CreatedAt:  "2026-05-07T11:00:00Z",
			UpdatedAt:  "2026-05-07T11:00:00Z",
		},
		[]workos.Role{{
			ID:          "role_01JSUPPORT",
			Name:        "Support",
			Slug:        roleSlug,
			Description: "Support operators",
			Type:        "OrganizationRole",
			CreatedAt:   "2026-05-07T11:00:00Z",
			UpdatedAt:   "2026-05-07T11:00:00Z",
		}},
		[]workos.Member{{
			ID:             membershipID,
			UserID:         workosUserID,
			OrganizationID: workosOrgID,
			Organization:   "Backfill User",
			RoleSlug:       roleSlug,
			Status:         "active",
			CreatedAt:      "2026-05-07T11:05:00Z",
			UpdatedAt:      "2026-05-07T11:05:00Z",
		}},
	)
	activity := NewBackfillWorkOSOrganization(logger, conn, workosClient)

	err := activity.Do(ctx, BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	role, err := accessrepo.New(conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
		OrganizationID: organizationID,
		WorkosSlug:     roleSlug,
	})
	require.NoError(t, err)

	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, fmt.Sprintf("role:organization:%s", role.ID.String()), assignments[0].RoleUrn)
	require.True(t, assignments[0].UserID.Valid)
	require.Equal(t, gramUserID, assignments[0].UserID.String)
	require.Equal(t, membershipID, assignments[0].WorkosMembershipID.String)
	require.Empty(t, assignments[0].WorkosLastEventID.String)

	relationship, err := orgrepo.New(conn).GetRelationshipByMembershipID(ctx, conv.ToPGText(membershipID))
	require.NoError(t, err)
	require.True(t, relationship.UserID.Valid)
	require.Equal(t, gramUserID, relationship.UserID.String)
	require.Equal(t, workosUserID, relationship.WorkosUserID.String)
}

func TestBackfillWorkOSOrganization_LinksExistingLocalUserByExternalID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newBackfillTestConn(t, "workos_backfill_existing_user_external_id")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_backfill_existing_user"
	const workosOrgID = "org_01JBACKFILLEXISTINGUSER"
	const workosUserID = "user_01JBACKFILLEXISTINGUSER"
	const gramUserID = "gram_user_01JBACKFILLEXISTINGUSER"
	const membershipID = "mem_01JBACKFILLEXISTINGUSER"

	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          gramUserID,
		Email:       "old@example.com",
		DisplayName: "Old Name",
		PhotoUrl:    conv.ToPGTextEmpty(""),
		Admin:       false,
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.UpsertOrganization(workos.Organization{
		ID:         workosOrgID,
		Name:       "Backfill Existing User",
		ExternalID: "",
		CreatedAt:  "2026-05-07T11:00:00Z",
		UpdatedAt:  "2026-05-07T11:00:00Z",
	})
	workosClient.UpsertUser(workosOrgID, workos.User{
		ID:                workosUserID,
		FirstName:         "Existing",
		LastName:          "User",
		Email:             "existing@example.com",
		ProfilePictureURL: "",
		ExternalID:        gramUserID,
		CreatedAt:         "2026-05-07T11:05:00Z",
		UpdatedAt:         "2026-05-07T11:05:00Z",
	})
	workosClient.UpsertOrganizationMembership(workos.Member{
		ID:             membershipID,
		UserID:         workosUserID,
		OrganizationID: workosOrgID,
		Organization:   "Backfill Existing User",
		RoleSlug:       "",
		Status:         "active",
		CreatedAt:      "2026-05-07T11:05:00Z",
		UpdatedAt:      "2026-05-07T11:05:00Z",
	})

	activity := NewBackfillWorkOSOrganization(logger, conn, workosClient)
	err = activity.Do(ctx, BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	user, err := usersrepo.New(conn).GetUser(ctx, gramUserID)
	require.NoError(t, err)
	require.Equal(t, "existing@example.com", user.Email)
	require.Equal(t, "Existing User", user.DisplayName)
	require.Equal(t, workosUserID, user.WorkosID.String)

	relationship, err := orgrepo.New(conn).GetRelationshipByMembershipID(ctx, conv.ToPGText(membershipID))
	require.NoError(t, err)
	require.True(t, relationship.UserID.Valid)
	require.Equal(t, gramUserID, relationship.UserID.String)
	require.Equal(t, workosUserID, relationship.WorkosUserID.String)
}

func TestBackfillWorkOSOrganization_MembershipWithNewerEventSkipsRoleSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newBackfillTestConn(t, "workos_backfill_membership_newer_event_wins")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_backfill_membership_event_wins"
	const workosOrgID = "org_01JBACKFILLMEMEVENT"
	const workosUserID = "user_01JBACKFILLMEMEVENT"
	const membershipID = "mem_01JBACKFILLMEMEVENT"
	const roleSlug = "org-member"

	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedOrganizationRoleWithCursor(t, ctx, conn, organizationID, roleSlug, "Member", "")
	err := orgrepo.New(conn).SyncUserOrganizationRoleAssignments(ctx, orgrepo.SyncUserOrganizationRoleAssignmentsParams{
		OrganizationID:     organizationID,
		WorkosUserID:       workosUserID,
		WorkosRoleSlugs:    []string{roleSlug},
		UserID:             conv.ToPGTextEmpty(""),
		WorkosMembershipID: conv.ToPGText(membershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)),
		WorkosLastEventID:  conv.ToPGText("event_99FRESH"),
	})
	require.NoError(t, err)

	workosClient := newWorkOSSnapshotClient(t, ctx,
		workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Membership Event Wins",
			ExternalID: "",
			CreatedAt:  "2026-05-07T11:00:00Z",
			UpdatedAt:  "2026-05-07T11:00:00Z",
		},
		[]workos.Role{{
			ID:          "role_01JMEMBER",
			Name:        "Member",
			Slug:        roleSlug,
			Description: "",
			Type:        "OrganizationRole",
			CreatedAt:   "2026-05-07T11:00:00Z",
			UpdatedAt:   "2026-05-07T11:00:00Z",
		}},
		[]workos.Member{{
			ID:             membershipID,
			UserID:         workosUserID,
			OrganizationID: workosOrgID,
			Organization:   "Backfill Membership Event Wins",
			RoleSlug:       "",
			Status:         "active",
			CreatedAt:      "2026-05-07T11:00:00Z",
			UpdatedAt:      "2026-05-07T11:00:00Z",
		}},
	)
	activity := NewBackfillWorkOSOrganization(logger, conn, workosClient)

	err = activity.Do(ctx, BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, "event_99FRESH", assignments[0].WorkosLastEventID.String)
}

func TestBackfillWorkOSOrganization_RoleWithLastEventIDSkipsSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newBackfillTestConn(t, "workos_backfill_role_last_event_wins")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_backfill_event_wins"
	const workosOrgID = "org_01JBACKFILLEVENTWINS"
	const roleSlug = "org-billing"

	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedOrganizationRoleWithCursor(t, ctx, conn, organizationID, roleSlug, "Billing From Event", "event_01JNEWER")

	workosClient := newWorkOSSnapshotClient(t, ctx,
		workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Event Wins",
			ExternalID: "",
			CreatedAt:  "2026-05-07T11:00:00Z",
			UpdatedAt:  "2026-05-07T11:00:00Z",
		},
		[]workos.Role{{
			ID:          "role_01JBILLING",
			Name:        "Billing From Snapshot",
			Slug:        roleSlug,
			Description: "",
			Type:        "OrganizationRole",
			CreatedAt:   "2026-05-07T11:00:00Z",
			UpdatedAt:   "2026-05-07T12:00:00Z",
		}},
		nil,
	)
	activity := NewBackfillWorkOSOrganization(logger, conn, workosClient)

	err := activity.Do(ctx, BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	role, err := accessrepo.New(conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
		OrganizationID: organizationID,
		WorkosSlug:     roleSlug,
	})
	require.NoError(t, err)
	require.Equal(t, "Billing From Event", role.WorkosName)
	require.Equal(t, "event_01JNEWER", role.WorkosLastEventID.String)
}

func TestBackfillWorkOSOrganization_MissingRoleSoftDeleted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newBackfillTestConn(t, "workos_backfill_role_deleted")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_backfill_delete_role"
	const workosOrgID = "org_01JBACKFILLDELETE"
	const roleSlug = "org-obsolete"

	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedOrganizationRoleWithCursor(t, ctx, conn, organizationID, roleSlug, "Obsolete", "")

	workosClient := newWorkOSSnapshotClient(t, ctx,
		workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Delete Role",
			ExternalID: "",
			CreatedAt:  "2026-05-07T11:00:00Z",
			UpdatedAt:  "2026-05-07T11:00:00Z",
		},
		nil,
		nil,
	)
	activity := NewBackfillWorkOSOrganization(logger, conn, workosClient)

	err := activity.Do(ctx, BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	role, err := accessrepo.New(conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
		OrganizationID: organizationID,
		WorkosSlug:     roleSlug,
	})
	require.NoError(t, err)
	require.True(t, role.Deleted)
	require.True(t, role.WorkosDeleted)
	require.Empty(t, role.WorkosLastEventID.String)
}

func newWorkOSSnapshotClient(t *testing.T, ctx context.Context, org workos.Organization, roles []workos.Role, members []workos.Member) *workos.StubClient {
	t.Helper()

	client := workos.NewStubClient()
	client.UpsertOrganization(org)
	for _, role := range roles {
		_, err := client.CreateRole(ctx, org.ID, workos.CreateRoleOpts{
			Name:        role.Name,
			Slug:        role.Slug,
			Description: role.Description,
		})
		require.NoError(t, err)
	}
	for _, member := range members {
		client.UpsertUser(org.ID, workos.User{
			ID:                member.UserID,
			FirstName:         "Test",
			LastName:          "User",
			Email:             member.UserID + "@example.com",
			ProfilePictureURL: "",
			ExternalID:        "gram_" + member.UserID,
			CreatedAt:         member.CreatedAt,
			UpdatedAt:         member.UpdatedAt,
		})
		client.UpsertOrganizationMembership(member)
	}

	return client
}

func seedLinkedWorkOSOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, workosOrgID string) {
	t.Helper()

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        organizationID,
		Slug:        organizationID,
		WorkosID:    conv.ToPGText(workosOrgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
}

func seedOrganizationRoleWithCursor(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID, slug, name, lastEventID string) {
	t.Helper()

	updatedAt := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	err := accessrepo.New(conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    organizationID,
		WorkosSlug:        slug,
		WorkosName:        name,
		WorkosDescription: conv.ToPGText(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(updatedAt),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(updatedAt),
		WorkosLastEventID: conv.ToPGText(lastEventID),
	})
	require.NoError(t, err)
}
