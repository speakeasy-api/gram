package agents

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agents/repo"
)

func TestAgentOwnerIsRequiredAndTenantPinned(t *testing.T) {
	conn := newTestDB(t)
	seedOrganization(t, conn, "org_one")
	seedOrganization(t, conn, "org_two")
	seedOrganizationUser(t, conn, "org_one", "user_one")
	seedOrganizationUser(t, conn, "org_two", "user_two")

	_, err := conn.Exec(t.Context(), `
		INSERT INTO agents (organization_id, owner_user_id, name)
		VALUES ($1, NULL, $2)`, "org_one", "Missing Owner")
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

	_, err = conn.Exec(t.Context(), `
		DELETE FROM organization_user_relationships
		WHERE organization_id = $1 AND user_id = $2`, "org_one", "user_one")
	require.Error(t, err)

	stored, err := repo.New(conn).GetAgentByID(t.Context(), repo.GetAgentByIDParams{
		OrganizationID: "org_one",
		ID:             agent.ID,
	})
	require.NoError(t, err)
	require.Equal(t, "user_one", stored.OwnerUserID)
}

func TestAgentNamesRemainReservedUntilDeletion(t *testing.T) {
	conn := newTestDB(t)
	seedOrganization(t, conn, "org_one")
	seedOrganization(t, conn, "org_two")
	seedOrganizationUser(t, conn, "org_one", "user_one")
	seedOrganizationUser(t, conn, "org_two", "user_two")
	q := repo.New(conn)

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
		return err
	}
	require.Error(t, createSameName(t))

	_, err = conn.Exec(t.Context(), `UPDATE agents SET suspended_at = clock_timestamp() WHERE id = $1`, agent.ID)
	require.NoError(t, err)
	require.Error(t, createSameName(t))

	_, err = conn.Exec(t.Context(), `
		UPDATE agents
		SET suspended_at = NULL, revoked_at = clock_timestamp()
		WHERE id = $1`, agent.ID)
	require.NoError(t, err)
	require.Error(t, createSameName(t))

	_, err = conn.Exec(t.Context(), `UPDATE agents SET deleted_at = clock_timestamp() WHERE id = $1`, agent.ID)
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
	conn := newTestDB(t)
	seedOrganization(t, conn, "org_one")
	seedOrganizationUser(t, conn, "org_one", "user_one")
	q := repo.New(conn)

	invalidLifecycle, err := q.CreateAgent(t.Context(), repo.CreateAgentParams{
		OrganizationID: "org_one",
		OwnerUserID:    "user_one",
		Name:           "Invalid Lifecycle",
	})
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `
		UPDATE agents
		SET suspended_at = clock_timestamp(), revoked_at = clock_timestamp()
		WHERE id = $1`, invalidLifecycle.ID)
	require.Error(t, err)

	timestampOnly, err := q.CreateAgent(t.Context(), repo.CreateAgentParams{
		OrganizationID: "org_one",
		OwnerUserID:    "user_one",
		Name:           "Timestamp Only Latch",
	})
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `
		UPDATE agents SET owner_reassignment_required_at = clock_timestamp()
		WHERE id = $1`, timestampOnly.ID)
	require.Error(t, err)

	reasonOnly, err := q.CreateAgent(t.Context(), repo.CreateAgentParams{
		OrganizationID: "org_one",
		OwnerUserID:    "user_one",
		Name:           "Reason Only Latch",
	})
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `
		UPDATE agents SET owner_reassignment_reason = 'owner unavailable'
		WHERE id = $1`, reasonOnly.ID)
	require.Error(t, err)

	for _, state := range []string{"active", "suspended", "revoked", "deleted"} {
		t.Run("latch with "+state, func(t *testing.T) {
			agent, err := q.CreateAgent(t.Context(), repo.CreateAgentParams{
				OrganizationID: "org_one",
				OwnerUserID:    "user_one",
				Name:           "Latched " + state,
			})
			require.NoError(t, err)

			switch state {
			case "suspended":
				_, err = conn.Exec(t.Context(), `UPDATE agents SET suspended_at = clock_timestamp() WHERE id = $1`, agent.ID)
			case "revoked":
				_, err = conn.Exec(t.Context(), `UPDATE agents SET revoked_at = clock_timestamp() WHERE id = $1`, agent.ID)
			case "deleted":
				_, err = conn.Exec(t.Context(), `UPDATE agents SET deleted_at = clock_timestamp() WHERE id = $1`, agent.ID)
			}
			require.NoError(t, err)

			_, err = conn.Exec(t.Context(), `
				UPDATE agents
				SET owner_reassignment_required_at = clock_timestamp(),
				    owner_reassignment_reason = 'owner unavailable'
				WHERE id = $1`, agent.ID)
			require.NoError(t, err)
		})
	}
}

func TestAgentSchemaStoresOnlyAuthoritativeFields(t *testing.T) {
	conn := newTestDB(t)
	rows, err := conn.Query(t.Context(), `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'agents'
		ORDER BY ordinal_position`)
	require.NoError(t, err)
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var column string
		require.NoError(t, rows.Scan(&column))
		columns = append(columns, column)
	}
	require.NoError(t, rows.Err())
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
