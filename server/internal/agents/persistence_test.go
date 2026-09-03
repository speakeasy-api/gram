package agents

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestAgentOwnerIsRequiredAndTenantPinned(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org_one")
	seedOrganization(t, conn, "org_two")
	seedOrganizationUser(t, conn, "org_one", "user_one")
	seedOrganizationUser(t, conn, "org_two", "user_two")
	fixtures := testrepo.New(conn)

	err := fixtures.CreateOwnerlessAgentFixture(t.Context(), testrepo.CreateOwnerlessAgentFixtureParams{
		OrganizationID: "org_one",
		Name:           "Missing Owner",
	})
	require.Error(t, err)

	_, err = repo.New(conn).CreateAgent(t.Context(), repo.CreateAgentParams{
		OrganizationID: "org_one",
		OwnerUserID:    "user_two",
		Name:           "Cross Tenant Owner",
	})
	require.Error(t, err)

	agent, err := repo.New(conn).CreateAgent(t.Context(), repo.CreateAgentParams{
		OrganizationID: "org_one",
		OwnerUserID:    "user_one",
		Name:           "Owned Agent",
	})
	require.NoError(t, err)
	require.Equal(t, "user_one", agent.OwnerUserID)

	err = fixtures.DeleteOrganizationUserRelationshipFixture(t.Context(), testrepo.DeleteOrganizationUserRelationshipFixtureParams{
		OrganizationID: "org_one",
		UserID:         conv.ToPGText("user_one"),
	})
	require.Error(t, err)

	stored, err := repo.New(conn).GetAgentByID(t.Context(), repo.GetAgentByIDParams{
		OrganizationID: "org_one",
		ID:             agent.ID,
	})
	require.NoError(t, err)
	require.Equal(t, "user_one", stored.OwnerUserID)
}

func TestAgentNamesRemainReservedUntilDeletion(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org_one")
	seedOrganization(t, conn, "org_two")
	seedOrganizationUser(t, conn, "org_one", "user_one")
	seedOrganizationUser(t, conn, "org_two", "user_two")
	q := repo.New(conn)
	fixtures := testrepo.New(conn)

	agent, err := q.CreateAgent(t.Context(), repo.CreateAgentParams{
		OrganizationID: "org_one",
		OwnerUserID:    "user_one",
		Name:           "Reserved Name",
	})
	require.NoError(t, err)

	createSameName := func(t *testing.T) error {
		t.Helper()
		_, err := q.CreateAgent(t.Context(), repo.CreateAgentParams{
			OrganizationID: "org_one",
			OwnerUserID:    "user_one",
			Name:           "reserved name",
		})
		if err != nil {
			return fmt.Errorf("create duplicate agent: %w", err)
		}
		return nil
	}
	require.Error(t, createSameName(t))

	err = fixtures.SetAgentSuspendedFixture(t.Context(), agent.ID)
	require.NoError(t, err)
	require.Error(t, createSameName(t))

	err = fixtures.SetAgentRevokedFixture(t.Context(), agent.ID)
	require.NoError(t, err)
	require.Error(t, createSameName(t))

	err = fixtures.SoftDeleteAgentFixture(t.Context(), agent.ID)
	require.NoError(t, err)
	require.NoError(t, createSameName(t))

	_, err = q.CreateAgent(t.Context(), repo.CreateAgentParams{
		OrganizationID: "org_two",
		OwnerUserID:    "user_two",
		Name:           "RESERVED NAME",
	})
	require.NoError(t, err)
}

func TestAgentLifecycleAndOwnerLatchConstraints(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	seedOrganization(t, conn, "org_one")
	seedOrganizationUser(t, conn, "org_one", "user_one")
	q := repo.New(conn)
	fixtures := testrepo.New(conn)

	invalidLifecycle, err := q.CreateAgent(t.Context(), repo.CreateAgentParams{
		OrganizationID: "org_one",
		OwnerUserID:    "user_one",
		Name:           "Invalid Lifecycle",
	})
	require.NoError(t, err)
	err = fixtures.SetAgentInvalidLifecycleFixture(t.Context(), invalidLifecycle.ID)
	require.Error(t, err)

	timestampOnly, err := q.CreateAgent(t.Context(), repo.CreateAgentParams{
		OrganizationID: "org_one",
		OwnerUserID:    "user_one",
		Name:           "Timestamp Only Latch",
	})
	require.NoError(t, err)
	err = fixtures.SetAgentOwnerLatchTimestampOnlyFixture(t.Context(), timestampOnly.ID)
	require.Error(t, err)

	reasonOnly, err := q.CreateAgent(t.Context(), repo.CreateAgentParams{
		OrganizationID: "org_one",
		OwnerUserID:    "user_one",
		Name:           "Reason Only Latch",
	})
	require.NoError(t, err)
	err = fixtures.SetAgentOwnerLatchReasonOnlyFixture(t.Context(), reasonOnly.ID)
	require.Error(t, err)

	for _, state := range []string{"active", "suspended", "revoked", "deleted"} {
		t.Run("latch with "+state, func(t *testing.T) {
			t.Parallel()

			agent, err := q.CreateAgent(t.Context(), repo.CreateAgentParams{
				OrganizationID: "org_one",
				OwnerUserID:    "user_one",
				Name:           "Latched " + state,
			})
			require.NoError(t, err)

			switch state {
			case "suspended":
				err = fixtures.SetAgentSuspendedFixture(t.Context(), agent.ID)
			case "revoked":
				err = fixtures.SetAgentRevokedFixture(t.Context(), agent.ID)
			case "deleted":
				err = fixtures.SoftDeleteAgentFixture(t.Context(), agent.ID)
			}
			require.NoError(t, err)

			err = fixtures.SetAgentOwnerLatchFixture(t.Context(), agent.ID)
			require.NoError(t, err)
		})
	}
}

func TestAgentSchemaStoresOnlyAuthoritativeFields(t *testing.T) {
	t.Parallel()

	conn := newTestDB(t)
	columns, err := testrepo.New(conn).ListAgentColumnNamesFixture(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{
		"id",
		"organization_id",
		"owner_user_id",
		"name",
		"suspended_at",
		"revoked_at",
		"owner_reassignment_required_at",
		"owner_reassignment_reason",
		"created_at",
		"updated_at",
		"deleted_at",
		"deleted",
	}, columns)
}
