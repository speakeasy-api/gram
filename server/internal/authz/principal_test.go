package authz

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
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
	require.NoError(t, SeedSystemRoleGrants(ctx, conn, organizationID))
	seedRoleAssignmentForUser(t, ctx, conn, organizationID, userID, SystemRoleMember)
	memberRole, err := accessrepo.New(conn).GetGlobalRoleBySlug(ctx, SystemRoleMember)
	require.NoError(t, err)

	principals, err := ResolveUserPrincipals(ctx, conn, organizationID, userID)
	require.NoError(t, err)

	principalURNs := make([]string, 0, len(principals))
	for _, principal := range principals {
		principalURNs = append(principalURNs, principal.String())
	}
	require.Contains(t, principalURNs, urn.NewPrincipal(urn.PrincipalTypeUser, userID).String())
	require.Contains(t, principalURNs, AllUsersPrincipal().String())
	require.Contains(t, principalURNs, "role:global:"+memberRole.ID.String())
	require.NotContains(t, principalURNs, "role:member")
}

func TestParseRolePrincipalURN_requiresCanonicalRoleURN(t *testing.T) {
	t.Parallel()

	const roleID = "00000000-0000-0000-0000-000000000001"
	for _, roleURN := range []string{"role:global:" + roleID, "role:organization:" + roleID} {
		principal, err := parseRolePrincipalURN(roleURN)
		require.NoError(t, err)
		require.Equal(t, roleURN, principal.String())
	}

	for _, roleURN := range []string{
		"role:member",
		"role:organization:not-a-uuid",
		"role:unknown:" + roleID,
		"user:" + roleID,
	} {
		principal, err := parseRolePrincipalURN(roleURN)
		require.Error(t, err)
		require.Equal(t, urn.Principal{}, principal)
	}
}

func TestResolveUserPrincipals_includesAllUsersWhenUserMissingOrNotInOrg(t *testing.T) {
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
		require.NoError(t, err)
		require.Equal(t, []urn.Principal{AllUsersPrincipal()}, principals)
	}

	principals, err := ResolveUserPrincipals(ctx, conn, organizationID, urn.AllUsersPrincipalID)
	require.ErrorIs(t, err, ErrPrincipalInvalid)
	require.Empty(t, principals)
}

func TestResolveKnownUserPrincipals_allUsersGrantAuthorizesOrgMember(t *testing.T) {
	t.Parallel()

	ctx := enterpriseTestCtx(t.Context())
	conn := newTestDB(t)
	organizationID := "org_resolve_all_users"
	userID := "user_all_users"
	policyID := "policy_123"

	seedOrganization(t, ctx, conn, organizationID)
	seedActiveOrganizationUser(t, ctx, conn, organizationID, userID)
	seedGrant(t, ctx, conn, organizationID, AllUsersPrincipal(), ScopeRiskPolicyEvaluate, policyID)

	principals, err := ResolveUserPrincipals(ctx, conn, organizationID, userID)
	require.NoError(t, err)
	grants, err := LoadGrants(ctx, conn, organizationID, principals)
	require.NoError(t, err)

	grant, _ := matchingGrant(grants, Check{Scope: ScopeRiskPolicyEvaluate, ResourceKind: "", ResourceID: policyID, Dimensions: nil}.expand())
	require.NotNil(t, grant)
}

func TestValidatePrincipal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_validate_principal"
	userID := "user_validate_principal"

	seedOrganization(t, ctx, conn, organizationID)
	seedActiveOrganizationUser(t, ctx, conn, organizationID, userID)

	now := time.Now().UTC()
	role, err := accessrepo.New(conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    organizationID,
		WorkosSlug:        "principal-validator",
		WorkosName:        "Principal Validator",
		WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	rolePrincipal, err := urn.ParsePrincipal(role.RoleUrn)
	require.NoError(t, err)

	require.NoError(t, ValidatePrincipal(ctx, conn, organizationID, AllUsersPrincipal()))
	require.NoError(t, ValidatePrincipal(ctx, conn, organizationID, urn.NewPrincipal(urn.PrincipalTypeUser, userID)))
	require.NoError(t, ValidatePrincipal(ctx, conn, organizationID, rolePrincipal))

	err = ValidatePrincipal(ctx, conn, organizationID, urn.NewPrincipal(urn.PrincipalTypeUser, "user_missing"))
	require.ErrorIs(t, err, ErrPrincipalNotFound)

	err = ValidatePrincipal(ctx, conn, organizationID, urn.NewPrincipal(urn.PrincipalTypeRole, "principal-validator"))
	require.ErrorContains(t, err, "invalid role principal")
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
