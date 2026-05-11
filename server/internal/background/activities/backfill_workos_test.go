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
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type stubWorkOSBackfillClient struct {
	org         workos.Organization
	orgs        []workos.Organization
	roles       []workos.Role
	globalRoles []workos.Role
	members     []workos.Member
}

func (s *stubWorkOSBackfillClient) GetOrganization(_ context.Context, _ string) (*workos.Organization, error) {
	return &s.org, nil
}

func (s *stubWorkOSBackfillClient) ListOrganizations(_ context.Context) ([]workos.Organization, error) {
	return s.orgs, nil
}

func (s *stubWorkOSBackfillClient) ListRoles(_ context.Context, _ string) ([]workos.Role, error) {
	return s.roles, nil
}

func (s *stubWorkOSBackfillClient) ListOrgMemberships(_ context.Context, _ string) ([]workos.Member, error) {
	return s.members, nil
}

func (s *stubWorkOSBackfillClient) ListGlobalRoles(_ context.Context) ([]workos.Role, error) {
	return s.globalRoles, nil
}

func TestBackfillWorkOSOrganization_CreatesUnlinkedOrganizationWithExternalID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	conn := newOrgEventsTestConn(t, "workos_backfill_create_org_external_id")
	logger := testenv.NewLogger(t)

	const organizationID = "gram_org_from_workos_external_id"
	const workosOrgID = "org_01JBACKFILLCREATE"

	activity := activities.NewBackfillWorkOSOrganization(logger, conn, &stubWorkOSBackfillClient{
		org: workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Created Org",
			ExternalID: organizationID,
			CreatedAt:  "2026-05-07T11:00:00Z",
			UpdatedAt:  "2026-05-07T11:00:00Z",
		},
		roles:   []workos.Role{},
		members: []workos.Member{},
	})

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

	activity := activities.NewBackfillWorkOSOrganization(logger, conn, &stubWorkOSBackfillClient{
		org: workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Immutable Org",
			ExternalID: changedExternalID,
			CreatedAt:  "2026-05-07T11:00:00Z",
			UpdatedAt:  "2026-05-07T11:00:00Z",
		},
		roles:   []workos.Role{},
		members: []workos.Member{},
	})

	err := activity.Do(ctx, activities.BackfillWorkOSOrganizationParams{WorkOSOrganizationID: workosOrgID})
	require.NoError(t, err)

	org, err := orgrepo.New(conn).GetOrganizationByWorkosID(ctx, conv.ToPGText(workosOrgID))
	require.NoError(t, err)
	require.Equal(t, organizationID, org.ID)
	require.Equal(t, "Backfill Immutable Org", org.Name)

	_, err = orgrepo.New(conn).GetOrganizationMetadata(ctx, changedExternalID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
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

	activity := activities.NewBackfillWorkOSOrganization(logger, conn, &stubWorkOSBackfillClient{
		org: workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Unknown User",
			ExternalID: "",
			CreatedAt:  "2026-05-07T11:00:00Z",
			UpdatedAt:  "2026-05-07T11:00:00Z",
		},
		roles: []workos.Role{{
			ID:          "role_01JSUPPORT",
			Name:        "Support",
			Slug:        roleSlug,
			Description: "Support operators",
			Type:        "OrganizationRole",
			CreatedAt:   "2026-05-07T11:00:00Z",
			UpdatedAt:   "2026-05-07T11:00:00Z",
		}},
		members: []workos.Member{{
			ID:             membershipID,
			UserID:         workosUserID,
			OrganizationID: workosOrgID,
			Organization:   "Backfill Unknown User",
			RoleSlug:       roleSlug,
			Status:         "active",
			CreatedAt:      "2026-05-07T11:05:00Z",
			UpdatedAt:      "2026-05-07T11:05:00Z",
		}},
	})

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

	activity := activities.NewBackfillWorkOSOrganization(logger, conn, &stubWorkOSBackfillClient{
		org: workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Event Wins",
			ExternalID: "",
			CreatedAt:  "2026-05-07T11:00:00Z",
			UpdatedAt:  "2026-05-07T11:00:00Z",
		},
		roles: []workos.Role{{
			ID:          "role_01JBILLING",
			Name:        "Billing From Snapshot",
			Slug:        roleSlug,
			Description: "",
			Type:        "OrganizationRole",
			CreatedAt:   "2026-05-07T11:00:00Z",
			UpdatedAt:   "2026-05-07T12:00:00Z",
		}},
	})

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

	activity := activities.NewBackfillWorkOSOrganization(logger, conn, &stubWorkOSBackfillClient{
		org: workos.Organization{
			ID:         workosOrgID,
			Name:       "Backfill Delete Role",
			ExternalID: "",
			CreatedAt:  "2026-05-07T11:00:00Z",
			UpdatedAt:  "2026-05-07T11:00:00Z",
		},
		roles: []workos.Role{},
	})

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
