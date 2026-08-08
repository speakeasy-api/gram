package access

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	userrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/stretchr/testify/require"
)

func TestListActiveOrganizationAdmins(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAccessService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)

	const (
		globalAdminUserID = "global-admin-user"
		orgAdminUserID    = "org-admin-user"
		memberUserID      = "member-user"
	)

	seedAdminQueryUser(t, ctx, ti, authCtx.ActiveOrganizationID, globalAdminUserID, "global-admin@example.test", "Global Admin", "workos-global-admin")
	seedAdminQueryUser(t, ctx, ti, authCtx.ActiveOrganizationID, orgAdminUserID, "org-admin@example.test", "Organization Admin", "workos-org-admin")
	seedAdminQueryUser(t, ctx, ti, authCtx.ActiveOrganizationID, memberUserID, "member@example.test", "Member", "workos-member")

	now := time.Now().UTC()
	queries := accessrepo.New(ti.conn)
	require.NoError(t, queries.UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{
		WorkosSlug:        "admin",
		WorkosName:        "Admin",
		WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	}))
	_, err := queries.UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    authCtx.ActiveOrganizationID,
		WorkosSlug:        "admin",
		WorkosName:        "Admin",
		WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	_, err = queries.UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    authCtx.ActiveOrganizationID,
		WorkosSlug:        "member",
		WorkosName:        "Member",
		WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	_, err = queries.UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID:     authCtx.ActiveOrganizationID,
		WorkosUserID:       "workos-global-admin",
		UserID:             conv.PtrToPGText(nil),
		WorkosMembershipID: conv.ToPGTextEmpty(""),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(now),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
		WorkosRoleSlug:     "admin",
	})
	require.NoError(t, err)
	seedAdminQueryRoleAssignment(t, ctx, queries, authCtx.ActiveOrganizationID, orgAdminUserID, "workos-org-admin", "admin", now)
	seedAdminQueryRoleAssignment(t, ctx, queries, authCtx.ActiveOrganizationID, memberUserID, "workos-member", "member", now)

	admins, err := queries.ListActiveOrganizationAdmins(ctx, authCtx.ActiveOrganizationID)
	require.NoError(t, err)
	require.Equal(t, []accessrepo.ListActiveOrganizationAdminsRow{
		{ID: globalAdminUserID, Email: "global-admin@example.test", DisplayName: "Global Admin"},
		{ID: orgAdminUserID, Email: "org-admin@example.test", DisplayName: "Organization Admin"},
	}, admins)

	admin, err := queries.GetActiveOrganizationAdmin(ctx, accessrepo.GetActiveOrganizationAdminParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText(orgAdminUserID),
	})
	require.NoError(t, err)
	require.Equal(t, accessrepo.GetActiveOrganizationAdminRow{
		ID:          orgAdminUserID,
		Email:       "org-admin@example.test",
		DisplayName: "Organization Admin",
	}, admin)

	_, err = queries.GetActiveOrganizationAdmin(ctx, accessrepo.GetActiveOrganizationAdminParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         conv.ToPGText(memberUserID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func seedAdminQueryUser(t *testing.T, ctx context.Context, ti *testInstance, organizationID, userID, email, displayName, workosUserID string) {
	t.Helper()

	_, err := userrepo.New(ti.conn).UpsertUser(ctx, userrepo.UpsertUserParams{
		ID:          userID,
		Email:       email,
		DisplayName: displayName,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	err = userrepo.New(ti.conn).OverwriteUserWorkosID(ctx, userrepo.OverwriteUserWorkosIDParams{
		ID:       userID,
		WorkosID: conv.ToPGText(workosUserID),
	})
	require.NoError(t, err)

	_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
}

func seedAdminQueryRoleAssignment(t *testing.T, ctx context.Context, queries *accessrepo.Queries, organizationID, userID, workosUserID, roleSlug string, updatedAt time.Time) {
	t.Helper()

	_, err := queries.UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID:     organizationID,
		WorkosUserID:       workosUserID,
		UserID:             conv.ToPGText(userID),
		WorkosMembershipID: conv.ToPGTextEmpty(""),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(updatedAt),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
		WorkosRoleSlug:     roleSlug,
	})
	require.NoError(t, err)
}
