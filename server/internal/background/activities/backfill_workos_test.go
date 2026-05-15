package activities_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestBackfillWorkOSOrganization_CreatesUnlinkedOrganizationWithExternalID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_create_org_external_id")
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
	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient)

	err := activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	org, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, organizationID, org.ID)
	require.Equal(t, "Backfill Created Org", org.Name)
	require.Equal(t, "backfill-created-org", org.Slug)
	require.Equal(t, workosOrgID, org.WorkosID.String)
	require.Empty(t, org.WorkosLastEventID.String)
}

func TestBackfillWorkOSOrganization_ExternalIDChangeDoesNotChangeOrganizationID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_external_id_immutable")
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
	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient)

	err := activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	org, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, organizationID, org.ID)
	require.Equal(t, "Backfill Immutable Org", org.Name)

	_, err = orgrepo.New(conn).GetOrganizationMetadata(ctx, changedExternalID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestBackfillWorkOSOrganization_SkipsMembershipWhenUserCannotBeBackfilled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_skip_membership_without_user")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_backfill_unknown_user"
	const workosOrgID = "org_01JBACKFILLUNKNOWN"
	const workosUserID = "user_01JBACKFILLUNKNOWN"
	const membershipID = "mem_01JBACKFILLUNKNOWN"
	const roleSlug = "org-support"

	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)

	workosClient := newWorkOSSnapshotClient(t, ctx,
		workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Unknown User",
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
			Organization:   "Backfill Unknown User",
			RoleSlug:       roleSlug,
			Status:         "active",
			CreatedAt:      "2026-05-07T11:05:00Z",
			UpdatedAt:      "2026-05-07T11:05:00Z",
		}},
	)
	workosClient.UpsertUser(workosOrgID, workos.User{
		ID:                workosUserID,
		ExternalID:        "",
		FirstName:         "Unknown",
		LastName:          "Member",
		Email:             "unknown-member@example.com",
		ProfilePictureURL: "",
		CreatedAt:         "2026-05-07T11:00:00Z",
		UpdatedAt:         "2026-05-07T11:00:00Z",
	})
	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient)

	err := activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	_, err = accessrepo.New(conn).GetOrganizationRoleBySlug(ctx, accessrepo.GetOrganizationRoleBySlugParams{
		OrganizationID: organizationID,
		WorkosSlug:     roleSlug,
	})
	require.NoError(t, err)

	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Empty(t, assignments)
}

func TestBackfillWorkOSOrganization_RoleWithLastEventIDSkipsSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_role_last_event_wins")
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
	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient)

	err := activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
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
	conn := newOrgEventsTestConn(t, "workos_backfill_role_deleted")
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
	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient)

	err := activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
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

func TestBackfillWorkOSOrganization_SkipsUserWithoutLocalID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_user_missing_local_id")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_backfill_user_missing_local_id"
	const workosOrgID = "org_01JBACKFILLUSERMISS"
	const workosUserID = "user_01JBACKFILLUSERMISS"
	const membershipID = "mem_01JBACKFILLUSERMISS"

	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	workosClient := workos.NewStubClient()
	workosClient.UpsertOrganization(workos.Organization{
		ID:         workosOrgID,
		Name:       "Backfill User Missing Local ID",
		ExternalID: organizationID,
		CreatedAt:  "2026-05-07T10:00:00Z",
		UpdatedAt:  "2026-05-07T10:00:00Z",
	})
	workosClient.UpsertUser(workosOrgID, workos.User{
		ID:                workosUserID,
		ExternalID:        "",
		FirstName:         "Backfill",
		LastName:          "User",
		Email:             "backfill-user@example.com",
		ProfilePictureURL: "",
		CreatedAt:         "2026-05-07T10:00:00Z",
		UpdatedAt:         "2026-05-07T12:00:00Z",
	})
	workosClient.UpsertOrganizationMembership(workos.Member{
		ID:             membershipID,
		UserID:         workosUserID,
		OrganizationID: workosOrgID,
		Organization:   "Backfill User Missing Local ID",
		RoleSlug:       "",
		Status:         "active",
		CreatedAt:      "2026-05-07T10:00:00Z",
		UpdatedAt:      "2026-05-07T12:00:00Z",
	})

	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient)
	err := activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	rows, err := usersrepo.New(conn).GetUsersByWorkosIDs(ctx, []string{workosUserID})
	require.NoError(t, err)
	require.Empty(t, rows)
	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Empty(t, assignments)
	require.Empty(t, workosClient.UserExternalIDUpdates())
}

func TestBackfillWorkOSOrganization_BackfillsUsersAndLinksOptimisticRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_user_links_optimistic")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_backfill_user_links"
	const workosOrgID = "org_01JBACKFILLUSERLINKS"
	const workosUserID = "user_01JBACKFILLUSERLINKS"
	const membershipID = "mem_01JBACKFILLUSERLINKS"
	const roleSlug = "support"
	const gramUserID = "gram_user_backfill_links"
	updatedAt := time.Date(2026, 5, 7, 11, 0, 0, 0, time.UTC)

	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	err := accessrepo.New(conn).UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{
		WorkosSlug:        roleSlug,
		WorkosName:        "Support",
		WorkosDescription: conv.ToPGText("Support"),
		WorkosCreatedAt:   conv.ToPGTimestamptz(updatedAt),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(updatedAt),
		WorkosLastEventID: conv.ToPGText(""),
	})
	require.NoError(t, err)
	err = orgrepo.New(conn).UpsertWorkOSMembership(ctx, orgrepo.UpsertWorkOSMembershipParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGTextEmpty(""),
		WorkosUserID:       conv.ToPGText(workosUserID),
		WorkosMembershipID: conv.ToPGText(membershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(updatedAt),
		WorkosLastEventID:  conv.ToPGText(""),
	})
	require.NoError(t, err)
	err = orgrepo.New(conn).SyncUserOrganizationRoleAssignments(ctx, orgrepo.SyncUserOrganizationRoleAssignmentsParams{
		OrganizationID:     organizationID,
		WorkosUserID:       workosUserID,
		UserID:             conv.ToPGTextEmpty(""),
		WorkosMembershipID: conv.ToPGText(membershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(updatedAt),
		WorkosLastEventID:  conv.ToPGText(""),
		WorkosRoleSlugs:    []string{roleSlug},
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.UpsertOrganization(workos.Organization{
		ID:         workosOrgID,
		Name:       "Backfill User Links",
		ExternalID: organizationID,
		CreatedAt:  "2026-05-07T10:00:00Z",
		UpdatedAt:  "2026-05-07T10:00:00Z",
	})
	workosClient.UpsertUser(workosOrgID, workos.User{
		ID:                workosUserID,
		ExternalID:        gramUserID,
		FirstName:         "Backfill",
		LastName:          "User",
		Email:             "backfill-user@example.com",
		ProfilePictureURL: "",
		CreatedAt:         "2026-05-07T10:00:00Z",
		UpdatedAt:         "2026-05-07T12:00:00Z",
	})
	workosClient.UpsertOrganizationMembership(workos.Member{
		ID:             membershipID,
		UserID:         workosUserID,
		OrganizationID: workosOrgID,
		Organization:   "Backfill User Links",
		RoleSlug:       roleSlug,
		Status:         "active",
		CreatedAt:      "2026-05-07T10:00:00Z",
		UpdatedAt:      "2026-05-07T12:00:00Z",
	})

	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient)
	err = activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := usersrepo.New(conn).GetUser(ctx, gramUserID)
	require.NoError(t, err)
	require.Equal(t, "backfill-user@example.com", row.Email)

	relationship, err := orgrepo.New(conn).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(gramUserID),
	})
	require.NoError(t, err)
	require.Equal(t, membershipID, relationship.WorkosMembershipID.String)

	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, gramUserID, assignments[0].UserID.String)
	require.Empty(t, workosClient.UserExternalIDUpdates())
}

func TestBackfillWorkOSOrganization_UsesExistingUserWhenExternalIDMissing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_existing_user_no_external_id")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_backfill_existing_user"
	const workosOrgID = "org_01JBACKFILLEXISTING"
	const workosUserID = "user_01JBACKFILLEXISTING"
	const membershipID = "mem_01JBACKFILLEXISTING"
	const roleSlug = "support"
	const existingUserID = "gram_user_existing_workos_id"
	seedTime := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)

	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	_, err := usersrepo.New(conn).UpsertSyncedUser(ctx, usersrepo.UpsertSyncedUserParams{
		ID:              existingUserID,
		Email:           "old-backfill-user@example.com",
		DisplayName:     "Old Backfill User",
		PhotoUrl:        conv.ToPGTextEmpty(""),
		WorkosID:        conv.ToPGText(workosUserID),
		WorkosCreatedAt: conv.ToPGTimestamptz(seedTime),
		WorkosUpdatedAt: conv.ToPGTimestamptz(seedTime),
	})
	require.NoError(t, err)
	err = accessrepo.New(conn).UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{
		WorkosSlug:        roleSlug,
		WorkosName:        "Support",
		WorkosDescription: conv.ToPGText("Support"),
		WorkosCreatedAt:   conv.ToPGTimestamptz(seedTime),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID: conv.ToPGText(""),
	})
	require.NoError(t, err)
	err = orgrepo.New(conn).UpsertWorkOSMembership(ctx, orgrepo.UpsertWorkOSMembershipParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGTextEmpty(""),
		WorkosUserID:       conv.ToPGText(workosUserID),
		WorkosMembershipID: conv.ToPGText(membershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID:  conv.ToPGText(""),
	})
	require.NoError(t, err)
	err = orgrepo.New(conn).SyncUserOrganizationRoleAssignments(ctx, orgrepo.SyncUserOrganizationRoleAssignmentsParams{
		OrganizationID:     organizationID,
		WorkosUserID:       workosUserID,
		UserID:             conv.ToPGTextEmpty(""),
		WorkosMembershipID: conv.ToPGText(membershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(seedTime),
		WorkosLastEventID:  conv.ToPGText(""),
		WorkosRoleSlugs:    []string{roleSlug},
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.UpsertOrganization(workos.Organization{
		ID:         workosOrgID,
		Name:       "Backfill Existing User",
		ExternalID: organizationID,
		CreatedAt:  "2026-05-07T10:00:00Z",
		UpdatedAt:  "2026-05-07T10:00:00Z",
	})
	workosClient.UpsertUser(workosOrgID, workos.User{
		ID:                workosUserID,
		ExternalID:        "",
		FirstName:         "Existing",
		LastName:          "User",
		Email:             "new-backfill-user@example.com",
		ProfilePictureURL: "",
		CreatedAt:         "2026-05-07T10:00:00Z",
		UpdatedAt:         "2026-05-07T12:00:00Z",
	})
	workosClient.UpsertOrganizationMembership(workos.Member{
		ID:             membershipID,
		UserID:         workosUserID,
		OrganizationID: workosOrgID,
		Organization:   "Backfill Existing User",
		RoleSlug:       roleSlug,
		Status:         "active",
		CreatedAt:      "2026-05-07T10:00:00Z",
		UpdatedAt:      "2026-05-07T12:00:00Z",
	})

	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient)
	err = activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := usersrepo.New(conn).GetUser(ctx, existingUserID)
	require.NoError(t, err)
	require.Equal(t, "new-backfill-user@example.com", row.Email)

	relationship, err := orgrepo.New(conn).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(existingUserID),
	})
	require.NoError(t, err)
	require.Equal(t, membershipID, relationship.WorkosMembershipID.String)

	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.Equal(t, existingUserID, assignments[0].UserID.String)
	require.Empty(t, workosClient.UserExternalIDUpdates())
}

func TestBackfillWorkOSOrganization_ExistingUserTakesPrecedenceOverExternalID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_existing_user_precedence")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_backfill_user_precedence"
	const workosOrgID = "org_01JBACKFILLPRECEDENCE"
	const workosUserID = "user_01JBACKFILLPRECEDENCE"
	const membershipID = "mem_01JBACKFILLPRECEDENCE"
	const existingUserID = "gram_user_existing_precedence"
	const externalID = "gram_user_workos_external_id"
	seedTime := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)

	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	_, err := usersrepo.New(conn).UpsertSyncedUser(ctx, usersrepo.UpsertSyncedUserParams{
		ID:              existingUserID,
		Email:           "old-precedence@example.com",
		DisplayName:     "Old Precedence User",
		PhotoUrl:        conv.ToPGTextEmpty(""),
		WorkosID:        conv.ToPGText(workosUserID),
		WorkosCreatedAt: conv.ToPGTimestamptz(seedTime),
		WorkosUpdatedAt: conv.ToPGTimestamptz(seedTime),
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.UpsertOrganization(workos.Organization{
		ID:         workosOrgID,
		Name:       "Backfill Existing User Precedence",
		ExternalID: organizationID,
		CreatedAt:  "2026-05-07T10:00:00Z",
		UpdatedAt:  "2026-05-07T10:00:00Z",
	})
	workosClient.UpsertUser(workosOrgID, workos.User{
		ID:                workosUserID,
		ExternalID:        externalID,
		FirstName:         "Existing",
		LastName:          "Precedence",
		Email:             "new-precedence@example.com",
		ProfilePictureURL: "",
		CreatedAt:         "2026-05-07T10:00:00Z",
		UpdatedAt:         "2026-05-07T12:00:00Z",
	})
	workosClient.UpsertOrganizationMembership(workos.Member{
		ID:             membershipID,
		UserID:         workosUserID,
		OrganizationID: workosOrgID,
		Organization:   "Backfill Existing User Precedence",
		RoleSlug:       "",
		Status:         "active",
		CreatedAt:      "2026-05-07T10:00:00Z",
		UpdatedAt:      "2026-05-07T12:00:00Z",
	})

	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient)
	err = activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := usersrepo.New(conn).GetUser(ctx, existingUserID)
	require.NoError(t, err)
	require.Equal(t, "new-precedence@example.com", row.Email)

	_, err = usersrepo.New(conn).GetUser(ctx, externalID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Empty(t, workosClient.UserExternalIDUpdates())
}

func TestBackfillWorkOSOrganization_SkipsExistingUserWithNewerWorkOSUpdatedAt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_existing_user_newer")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_backfill_user_newer"
	const workosOrgID = "org_01JBACKFILLNEWER"
	const workosUserID = "user_01JBACKFILLNEWER"
	const membershipID = "mem_01JBACKFILLNEWER"
	const existingUserID = "gram_user_existing_newer"
	createdAt := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	newerUpdatedAt := time.Date(2026, 5, 7, 13, 0, 0, 0, time.UTC)

	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	_, err := usersrepo.New(conn).UpsertSyncedUser(ctx, usersrepo.UpsertSyncedUserParams{
		ID:              existingUserID,
		Email:           "newer-local@example.com",
		DisplayName:     "Newer Local User",
		PhotoUrl:        conv.ToPGTextEmpty(""),
		WorkosID:        conv.ToPGText(workosUserID),
		WorkosCreatedAt: conv.ToPGTimestamptz(createdAt),
		WorkosUpdatedAt: conv.ToPGTimestamptz(newerUpdatedAt),
	})
	require.NoError(t, err)

	workosClient := workos.NewStubClient()
	workosClient.UpsertOrganization(workos.Organization{
		ID:         workosOrgID,
		Name:       "Backfill Existing User Newer",
		ExternalID: organizationID,
		CreatedAt:  "2026-05-07T10:00:00Z",
		UpdatedAt:  "2026-05-07T10:00:00Z",
	})
	workosClient.UpsertUser(workosOrgID, workos.User{
		ID:                workosUserID,
		ExternalID:        existingUserID,
		FirstName:         "Older",
		LastName:          "Snapshot",
		Email:             "older-snapshot@example.com",
		ProfilePictureURL: "",
		CreatedAt:         "2026-05-07T10:00:00Z",
		UpdatedAt:         "2026-05-07T12:00:00Z",
	})
	workosClient.UpsertOrganizationMembership(workos.Member{
		ID:             membershipID,
		UserID:         workosUserID,
		OrganizationID: workosOrgID,
		Organization:   "Backfill Existing User Newer",
		RoleSlug:       "",
		Status:         "active",
		CreatedAt:      "2026-05-07T10:00:00Z",
		UpdatedAt:      "2026-05-07T12:00:00Z",
	})

	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient)
	err = activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	row, err := usersrepo.New(conn).GetUser(ctx, existingUserID)
	require.NoError(t, err)
	require.Equal(t, "newer-local@example.com", row.Email)
	require.True(t, row.WorkosUpdatedAt.Valid)
	require.True(t, row.WorkosUpdatedAt.Time.Equal(newerUpdatedAt))
	require.Empty(t, workosClient.UserExternalIDUpdates())
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
