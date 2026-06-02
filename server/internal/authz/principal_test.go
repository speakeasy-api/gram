package authz

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestResolveKnownUserPrincipals_resolvesUserAndRolesForOrgMember(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_resolve_principals"
	userID := "user_123"

	seedOrganization(t, ctx, conn, organizationID)
	seedActiveOrganizationUser(t, ctx, conn, organizationID, userID)
	require.NoError(t, SeedSystemRoleGrants(ctx, testenv.NewLogger(t), conn, organizationID))
	seedRoleAssignmentForUser(t, ctx, conn, organizationID, userID, SystemRoleMember)

	principals, err := ResolveUserPrincipals(ctx, conn, organizationID, userID)
	require.NoError(t, err)

	principalURNs := make([]string, 0, len(principals))
	for _, principal := range principals {
		principalURNs = append(principalURNs, principal.String())
	}
	require.Contains(t, principalURNs, urn.NewPrincipal(urn.PrincipalTypeUser, userID).String())
	require.Contains(t, principalURNs, "role:member")
	require.True(t, slices.ContainsFunc(principalURNs, func(principalURN string) bool {
		return strings.HasPrefix(principalURN, "role:global:")
	}))
}

func TestResolveKnownUserPrincipals_unidentifiedWhenUserMissingOrNotInOrg(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_resolve_principals_missing"
	otherOrganizationID := "org_resolve_principals_other"
	otherOrgUserID := "user_other_org"

	seedOrganization(t, ctx, conn, organizationID)
	seedOrganization(t, ctx, conn, otherOrganizationID)
	seedActiveOrganizationUser(t, ctx, conn, otherOrganizationID, otherOrgUserID)

	for _, userID := range []string{"", "user_missing", otherOrgUserID} {
		principals, err := ResolveUserPrincipals(ctx, conn, organizationID, userID)
		require.ErrorIs(t, err, ErrPrincipalNotFound)
		require.Empty(t, principals)
	}
}

func seedActiveOrganizationUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, userID string) {
	t.Helper()

	_, err := usersrepo.New(conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       userID + "@example.com",
		DisplayName: userID,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)

	_, err = orgrepo.New(conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
}

func seedRoleAssignmentForUser(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, userID string, roleSlug string) {
	t.Helper()

	_, err := accessrepo.New(conn).UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID:     organizationID,
		WorkosUserID:       userID,
		UserID:             conv.ToPGText(userID),
		WorkosMembershipID: conv.ToPGText("membership_" + userID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
		WorkosRoleSlug:     roleSlug,
	})
	require.NoError(t, err)
}
