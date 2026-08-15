package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
)

func TestResolveOrganizationAdminEmails_UsesEffectiveGrantsAndDeterministicDedupe(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newTestDB(t)
	organizationID := "org-admin-recipients"
	seedOrganization(t, ctx, db, organizationID)
	require.NoError(t, SeedSystemRoleGrants(ctx, db, organizationID))

	seedActiveOrganizationUser(t, ctx, db, organizationID, "admin-alpha")
	seedRoleAssignmentForUser(t, ctx, db, organizationID, "admin-alpha", SystemRoleAdmin)
	seedActiveOrganizationUser(t, ctx, db, organizationID, "admin-alpha-duplicate")
	seedRoleAssignmentForUser(t, ctx, db, organizationID, "admin-alpha-duplicate", SystemRoleAdmin)
	seedActiveOrganizationUser(t, ctx, db, organizationID, "admin-zeta")
	seedGrant(t, ctx, db, organizationID, urn.NewPrincipal(urn.PrincipalTypeUser, "admin-zeta"), ScopeOrgAdmin, organizationID)
	seedActiveOrganizationUser(t, ctx, db, organizationID, "ordinary-member")
	seedRoleAssignmentForUser(t, ctx, db, organizationID, "ordinary-member", SystemRoleMember)

	for userID, email := range map[string]string{
		"admin-alpha":           "Alpha@example.test",
		"admin-alpha-duplicate": "alpha@EXAMPLE.TEST",
		"admin-zeta":            "zeta@example.test",
	} {
		_, err := usersrepo.New(db).UpsertUser(ctx, usersrepo.UpsertUserParams{
			ID:          userID,
			Email:       email,
			DisplayName: userID,
			PhotoUrl:    conv.PtrToPGText(nil),
			Admin:       false,
		})
		require.NoError(t, err)
	}

	recipients, err := ResolveOrganizationAdminEmails(ctx, db, organizationID)

	require.NoError(t, err)
	require.Equal(t, []string{"alpha@EXAMPLE.TEST", "zeta@example.test"}, recipients)
}

func TestResolveOrganizationAdminEmails_ReturnsPartialAudienceWithResolutionError(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newTestDB(t)
	organizationID := "org-admin-recipients-partial"
	seedOrganization(t, ctx, db, organizationID)
	require.NoError(t, SeedSystemRoleGrants(ctx, db, organizationID))
	seedActiveOrganizationUser(t, ctx, db, organizationID, "good-admin")
	seedRoleAssignmentForUser(t, ctx, db, organizationID, "good-admin", SystemRoleAdmin)
	seedActiveOrganizationUser(t, ctx, db, organizationID, urn.AllUsersPrincipalID)

	recipients, err := ResolveOrganizationAdminEmails(ctx, db, organizationID)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrPrincipalInvalid)
	require.Equal(t, []string{"good-admin@example.com"}, recipients)
}
