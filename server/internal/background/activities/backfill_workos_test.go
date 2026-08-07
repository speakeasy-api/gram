package activities_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
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
	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient, cache.NoopCache)

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
	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient, cache.NoopCache)

	err := activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	org, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, organizationID, org.ID)
	require.Equal(t, "Backfill Immutable Org", org.Name)

	_, err = orgrepo.New(conn).GetOrganizationMetadata(ctx, changedExternalID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestBackfillWorkOSOrganization_DoesNotClearDisabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_preserves_disabled")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_backfill_disabled"
	const workosOrgID = "org_01JBACKFILLDISABLED"

	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	_, err := orgrepo.New(conn).DisableOrganizationByWorkosID(ctx, orgrepo.DisableOrganizationByWorkosIDParams{
		WorkosID:          conv.ToPGText(workosOrgID),
		WorkosLastEventID: conv.ToPGText(""),
	})
	require.NoError(t, err)

	workosClient := newWorkOSSnapshotClient(t, ctx,
		workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Still Disabled",
			ExternalID: organizationID,
			CreatedAt:  "2026-05-07T11:00:00Z",
			UpdatedAt:  "2026-05-07T12:00:00Z",
		},
		nil,
		nil,
	)
	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient, cache.NoopCache)

	err = activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	org, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, "Backfill Still Disabled", org.Name)
	require.True(t, org.DisabledAt.Valid)
}

func TestBackfillWorkOSOrganization_UnknownUserSyncsSingleRoleAssignment(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_unknown_user_single_role")
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
			RoleSlugs:      []string{roleSlug},
			Status:         "active",
			CreatedAt:      "2026-05-07T11:05:00Z",
			UpdatedAt:      "2026-05-07T11:05:00Z",
		}},
	)
	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient, cache.NoopCache)

	err := activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
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
	require.False(t, assignments[0].UserID.Valid)
	require.Equal(t, membershipID, assignments[0].WorkosMembershipID.String)
	require.Empty(t, assignments[0].WorkosLastEventID.String)

	relationship, err := orgrepo.New(conn).GetRelationshipByMembershipID(ctx, conv.ToPGText(membershipID))
	require.NoError(t, err)
	require.False(t, relationship.UserID.Valid)
	require.Equal(t, workosUserID, relationship.WorkosUserID.String)
}

func TestBackfillWorkOSOrganization_MembershipWithNewerEventSkipsRoleSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_membership_newer_event_wins")
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
			RoleSlugs:      nil,
			Status:         "active",
			CreatedAt:      "2026-05-07T11:00:00Z",
			UpdatedAt:      "2026-05-07T11:00:00Z",
		}},
	)
	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient, cache.NoopCache)

	err = activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
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
	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient, cache.NoopCache)

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
	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient, cache.NoopCache)

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

func TestBackfillWorkOSOrganization_InactiveDirectoryUserDeprovisionsAccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_directory_user_inactive")
	logger := testenv.NewLogger(t)

	const (
		organizationID  = "gram_org_backfill_dsync_inactive"
		workosOrgID     = "org_01JBACKFILLDSYNCINACT"
		userID          = "user_backfill_dsync_inactive"
		workosUserID    = "user_01JBACKFILLDSYNCINACT"
		membershipID    = "mem_01JBACKFILLDSYNCINACT"
		directoryID     = "directory_01JBACKFILLINACT"
		directoryUserID = "directory_user_backfill_inactive"
	)
	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedWorkOSUser(t, ctx, conn, userID, workosUserID)
	seedOrganizationRole(t, ctx, conn, organizationID, "member")
	email := userID + "@example.com"

	// Seed the state the pre-state-check event handlers left behind: a live
	// relationship, role assignments, and a directory user row whose cursor
	// was advanced by an event that ignored state=inactive.
	relationshipUpdatedAt := time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)
	err := orgrepo.New(conn).UpsertWorkOSMembership(ctx, orgrepo.UpsertWorkOSMembershipParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGText(userID),
		WorkosUserID:       conv.ToPGText(workosUserID),
		WorkosMembershipID: conv.ToPGText(membershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(relationshipUpdatedAt),
		WorkosLastEventID:  conv.ToPGText("event_00SEED"),
	})
	require.NoError(t, err)
	err = orgrepo.New(conn).SyncUserOrganizationRoleAssignments(ctx, orgrepo.SyncUserOrganizationRoleAssignmentsParams{
		OrganizationID:     organizationID,
		WorkosUserID:       workosUserID,
		UserID:             conv.ToPGText(userID),
		WorkosMembershipID: conv.ToPGText(membershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(relationshipUpdatedAt),
		WorkosLastEventID:  conv.ToPGText("event_00SEED"),
		WorkosRoleSlugs:    []string{"member"},
	})
	require.NoError(t, err)
	_, err = workosrepo.New(conn).UpsertDirectoryUser(ctx, workosrepo.UpsertDirectoryUserParams{
		OrganizationID:        organizationID,
		UserID:                conv.ToPGText(userID),
		WorkosDirectoryUserID: directoryUserID,
		Email:                 conv.ToPGText(email),
		Attributes:            []byte(`{}`),
		RestoreDeleted:        false,
		WorkosCreatedAt:       conv.ToPGTimestamptz(relationshipUpdatedAt),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(relationshipUpdatedAt),
		WorkosLastEventID:     conv.ToPGText("event_00SEED"),
	})
	require.NoError(t, err)

	workosClient := newWorkOSSnapshotClient(t, ctx,
		workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Directory Inactive",
			ExternalID: organizationID,
			CreatedAt:  "2026-05-12T11:00:00Z",
			UpdatedAt:  "2026-05-12T11:00:00Z",
		},
		nil,
		nil,
	)
	workosClient.SetDirectories(workosOrgID, workos.Directory{
		ID:             directoryID,
		OrganizationID: workosOrgID,
		Type:           "okta scim v2.0",
		Name:           "Okta",
		State:          "linked",
		CreatedAt:      "2026-05-12T11:00:00Z",
		UpdatedAt:      "2026-05-12T11:00:00Z",
	})
	workosClient.SetDirectoryUsers(directoryID, workos.DirectoryUser{
		ID:               directoryUserID,
		DirectoryID:      directoryID,
		OrganizationID:   workosOrgID,
		Email:            email,
		State:            "inactive",
		CustomAttributes: nil,
		CreatedAt:        "2026-05-12T12:00:00Z",
		UpdatedAt:        "2026-05-12T12:00:00Z",
	})

	capturingCache := newCaptureCache()
	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient, capturingCache)

	err = activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	// The directory user row is soft-deleted despite its advanced event
	// cursor — the snapshot repair guard is timestamp-only.
	_, err = workosrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, directoryUserID)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	relationship, err := orgrepo.New(conn).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
	require.True(t, relationship.Deleted)
	// The deprovision is stamped with the snapshot's updated_at so delayed
	// membership events that are genuinely newer still apply.
	require.Equal(t, time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC), relationship.WorkosUpdatedAt.Time.UTC())

	assignments, err := orgrepo.New(conn).ListOrganizationRoleAssignmentsByWorkOSUser(ctx, orgrepo.ListOrganizationRoleAssignmentsByWorkOSUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	require.True(t, assignments[0].DeletedAt.Valid)

	deletedKeys := capturingCache.Deleted()
	require.Len(t, deletedKeys, 1)
	require.Contains(t, deletedKeys[0], sessions.UserInfoCacheKey(userID))
}

func TestBackfillWorkOSOrganization_SoftDeletedRowEmailMismatchStillDeprovisions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_directory_user_email_mismatch")
	logger := testenv.NewLogger(t)

	const (
		organizationID  = "gram_org_backfill_dsync_mismatch"
		workosOrgID     = "org_01JBACKFILLDSYNCMISM"
		userID          = "user_backfill_dsync_mismatch"
		workosUserID    = "user_01JBACKFILLDSYNCMISM"
		membershipID    = "mem_01JBACKFILLDSYNCMISM"
		directoryID     = "directory_01JBACKFILLMISM"
		directoryUserID = "directory_user_backfill_mismatch"
	)
	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedWorkOSUser(t, ctx, conn, userID, workosUserID)

	err := orgrepo.New(conn).UpsertWorkOSMembership(ctx, orgrepo.UpsertWorkOSMembershipParams{
		OrganizationID:     organizationID,
		UserID:             conv.ToPGText(userID),
		WorkosUserID:       conv.ToPGText(workosUserID),
		WorkosMembershipID: conv.ToPGText(membershipID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)),
		WorkosLastEventID:  conv.ToPGText("event_00SEED"),
	})
	require.NoError(t, err)

	// A directory row that was soft-deleted by an earlier deactivation whose
	// deprovision was skipped (the directory email no longer matches the
	// Gram user). The stored user_id is the only remaining linkage.
	_, err = workosrepo.New(conn).UpsertDirectoryUser(ctx, workosrepo.UpsertDirectoryUserParams{
		OrganizationID:        organizationID,
		UserID:                conv.ToPGText(userID),
		WorkosDirectoryUserID: directoryUserID,
		Email:                 conv.ToPGText("old." + userID + "@example.com"),
		Attributes:            []byte(`{}`),
		RestoreDeleted:        false,
		WorkosCreatedAt:       conv.ToPGTimestamptz(time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)),
		WorkosLastEventID:     conv.ToPGText("event_00SEED"),
	})
	require.NoError(t, err)
	_, err = workosrepo.New(conn).DeleteDirectoryUserByWorkOSID(ctx, workosrepo.DeleteDirectoryUserByWorkOSIDParams{
		WorkosDeletedAt:       conv.ToPGTimestamptz(time.Date(2026, 5, 12, 12, 30, 0, 0, time.UTC)),
		WorkosLastEventID:     conv.ToPGText("event_00SEED"),
		WorkosDirectoryUserID: directoryUserID,
	})
	require.NoError(t, err)

	workosClient := newWorkOSSnapshotClient(t, ctx,
		workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Directory Mismatch",
			ExternalID: organizationID,
			CreatedAt:  "2026-05-12T11:00:00Z",
			UpdatedAt:  "2026-05-12T11:00:00Z",
		},
		nil,
		nil,
	)
	workosClient.SetDirectories(workosOrgID, workos.Directory{
		ID:             directoryID,
		OrganizationID: workosOrgID,
		Type:           "okta scim v2.0",
		Name:           "Okta",
		State:          "linked",
		CreatedAt:      "2026-05-12T11:00:00Z",
		UpdatedAt:      "2026-05-12T11:00:00Z",
	})
	workosClient.SetDirectoryUsers(directoryID, workos.DirectoryUser{
		ID:               directoryUserID,
		DirectoryID:      directoryID,
		OrganizationID:   workosOrgID,
		Email:            "renamed." + userID + "@example.com",
		State:            "inactive",
		CustomAttributes: nil,
		CreatedAt:        "2026-05-12T12:00:00Z",
		UpdatedAt:        "2026-05-12T13:00:00Z",
	})

	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient, cache.NoopCache)

	err = activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	relationship, err := orgrepo.New(conn).GetOrganizationRelationshipForUser(ctx, orgrepo.GetOrganizationRelationshipForUserParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
	require.True(t, relationship.Deleted)
}

func TestBackfillWorkOSOrganization_ActiveDirectoryUserUpserted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_directory_user_active")
	logger := testenv.NewLogger(t)

	const (
		organizationID  = "gram_org_backfill_dsync_active"
		workosOrgID     = "org_01JBACKFILLDSYNCACT"
		userID          = "user_backfill_dsync_active"
		workosUserID    = "user_01JBACKFILLDSYNCACT"
		directoryID     = "directory_01JBACKFILLACT"
		directoryUserID = "directory_user_backfill_active"
	)
	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedWorkOSUser(t, ctx, conn, userID, workosUserID)
	email := userID + "@example.com"

	workosClient := newWorkOSSnapshotClient(t, ctx,
		workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Directory Active",
			ExternalID: organizationID,
			CreatedAt:  "2026-05-12T11:00:00Z",
			UpdatedAt:  "2026-05-12T11:00:00Z",
		},
		nil,
		nil,
	)
	workosClient.SetDirectories(workosOrgID, workos.Directory{
		ID:             directoryID,
		OrganizationID: workosOrgID,
		Type:           "okta scim v2.0",
		Name:           "Okta",
		State:          "linked",
		CreatedAt:      "2026-05-12T11:00:00Z",
		UpdatedAt:      "2026-05-12T11:00:00Z",
	})
	workosClient.SetDirectoryUsers(directoryID, workos.DirectoryUser{
		ID:               directoryUserID,
		DirectoryID:      directoryID,
		OrganizationID:   workosOrgID,
		Email:            email,
		State:            "active",
		CustomAttributes: []byte(`{"department":"Engineering"}`),
		CreatedAt:        "2026-05-12T12:00:00Z",
		UpdatedAt:        "2026-05-12T12:00:00Z",
	})

	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient, cache.NoopCache)

	err := activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	directoryUser, err := workosrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, directoryUserID)
	require.NoError(t, err)
	require.False(t, directoryUser.Deleted)
	require.Equal(t, userID, directoryUser.UserID.String)
	require.JSONEq(t, `{"department":"Engineering"}`, string(directoryUser.Attributes))
	require.Empty(t, directoryUser.WorkosLastEventID.String)
}

func TestBackfillWorkOSOrganization_StaleDirectoryUserSnapshotSkipped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_directory_user_stale")
	logger := testenv.NewLogger(t)

	const (
		organizationID  = "gram_org_backfill_dsync_stale"
		workosOrgID     = "org_01JBACKFILLDSYNCSTALE"
		userID          = "user_backfill_dsync_stale"
		workosUserID    = "user_01JBACKFILLDSYNCSTALE"
		directoryID     = "directory_01JBACKFILLSTALE"
		directoryUserID = "directory_user_backfill_stale"
	)
	seedLinkedWorkOSOrganization(t, ctx, conn, organizationID, workosOrgID)
	seedWorkOSUser(t, ctx, conn, userID, workosUserID)
	email := userID + "@example.com"

	// The local row is newer than the snapshot, so the backfill must not
	// touch it.
	_, err := workosrepo.New(conn).UpsertDirectoryUser(ctx, workosrepo.UpsertDirectoryUserParams{
		OrganizationID:        organizationID,
		UserID:                conv.ToPGText(userID),
		WorkosDirectoryUserID: directoryUserID,
		Email:                 conv.ToPGText(email),
		Attributes:            []byte(`{}`),
		RestoreDeleted:        false,
		WorkosCreatedAt:       conv.ToPGTimestamptz(time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC)),
		WorkosUpdatedAt:       conv.ToPGTimestamptz(time.Date(2026, 5, 12, 14, 0, 0, 0, time.UTC)),
		WorkosLastEventID:     conv.ToPGText("event_00SEED"),
	})
	require.NoError(t, err)

	workosClient := newWorkOSSnapshotClient(t, ctx,
		workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Directory Stale",
			ExternalID: organizationID,
			CreatedAt:  "2026-05-12T11:00:00Z",
			UpdatedAt:  "2026-05-12T11:00:00Z",
		},
		nil,
		nil,
	)
	workosClient.SetDirectories(workosOrgID, workos.Directory{
		ID:             directoryID,
		OrganizationID: workosOrgID,
		Type:           "okta scim v2.0",
		Name:           "Okta",
		State:          "linked",
		CreatedAt:      "2026-05-12T11:00:00Z",
		UpdatedAt:      "2026-05-12T11:00:00Z",
	})
	workosClient.SetDirectoryUsers(directoryID, workos.DirectoryUser{
		ID:               directoryUserID,
		DirectoryID:      directoryID,
		OrganizationID:   workosOrgID,
		Email:            email,
		State:            "inactive",
		CustomAttributes: nil,
		CreatedAt:        "2026-05-12T12:00:00Z",
		UpdatedAt:        "2026-05-12T13:00:00Z",
	})

	activity := activities.NewBackfillWorkOSOrganization(logger, conn, workosClient, cache.NoopCache)

	err = activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	directoryUser, err := workosrepo.New(conn).GetDirectoryUserByWorkOSID(ctx, directoryUserID)
	require.NoError(t, err)
	require.False(t, directoryUser.Deleted)
	require.Equal(t, "event_00SEED", directoryUser.WorkosLastEventID.String)
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
	_, err := accessrepo.New(conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
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
