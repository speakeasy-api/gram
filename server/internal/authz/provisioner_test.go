package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestProvisionOrganizationAdminWithoutWorkOSUserIDSeedsGrantsWithoutAssignment(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_provision_without_workos_user"
	seedOrganization(t, ctx, conn, organizationID)

	provisioner := NewProvisioner(conn)
	err := provisioner.ProvisionOrganizationAdmin(ctx, organizationID, InitialOrganizationAdmin{
		UserID:             "user_test",
		WorkOSUserID:       "",
		WorkOSMembershipID: "",
	})
	require.NoError(t, err)

	adminRole, err := repo.New(conn).GetGlobalRoleBySlug(ctx, SystemRoleAdmin)
	require.NoError(t, err)
	grants, err := repo.New(conn).GetPrincipalGrants(ctx, repo.GetPrincipalGrantsParams{
		OrganizationID: organizationID,
		PrincipalUrns:  []string{"role:global:" + adminRole.ID.String()},
	})
	require.NoError(t, err)
	require.NotEmpty(t, grants)

	assignments, err := repo.New(conn).ListOrganizationRoleAssignmentRecordsByWorkosUser(ctx, repo.ListOrganizationRoleAssignmentRecordsByWorkosUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   "user_test",
	})
	require.NoError(t, err)
	require.Empty(t, assignments)
}

func TestProvisionOrganizationAdminTxUsesCallerTransaction(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn := newTestDB(t)
	organizationID := "org_provision_transaction"
	workosUserID := "workos_user_transaction"
	seedOrganization(t, ctx, conn, organizationID)
	seedConnectedUser(t, ctx, conn, organizationID, "user_transaction", "user@example.com", "Test User", workosUserID, "membership_transaction")

	tx := testenv.BeginTx(t, ctx, conn)

	provisioner := NewProvisioner(conn)
	err := provisioner.ProvisionOrganizationAdminTx(ctx, tx, organizationID, InitialOrganizationAdmin{
		UserID:             "user_transaction",
		WorkOSUserID:       workosUserID,
		WorkOSMembershipID: "membership_transaction",
	})
	require.NoError(t, err)

	adminRole, err := repo.New(tx).GetGlobalRoleBySlug(ctx, SystemRoleAdmin)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))

	grants, err := repo.New(conn).GetPrincipalGrants(ctx, repo.GetPrincipalGrantsParams{
		OrganizationID: organizationID,
		PrincipalUrns:  []string{"role:global:" + adminRole.ID.String()},
	})
	require.NoError(t, err)
	require.Empty(t, grants)

	assignments, err := repo.New(conn).ListOrganizationRoleAssignmentRecordsByWorkosUser(ctx, repo.ListOrganizationRoleAssignmentRecordsByWorkosUserParams{
		OrganizationID: organizationID,
		WorkosUserID:   workosUserID,
	})
	require.NoError(t, err)
	require.Empty(t, assignments)
}
