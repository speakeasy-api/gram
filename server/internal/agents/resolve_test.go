package agents

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestResolvePrincipalIsTenantScopedAndFailClosed(t *testing.T) {
	conn := newTestDB(t)
	seedOrganization(t, conn, "org_one")
	seedOrganization(t, conn, "org_two")
	seedOrganizationUser(t, conn, "org_one", "user_one")

	agent, err := repo.New(conn).CreateAgent(t.Context(), repo.CreateAgentParams{
		OrganizationID: "org_one",
		OwnerUserID:    "user_one",
		Name:           "Resolver Agent",
	})
	require.NoError(t, err)
	principal := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())

	resolved, err := ResolvePrincipal(t.Context(), conn, "org_one", principal)
	require.NoError(t, err)
	require.Equal(t, agent.ID, resolved.ID)

	_, err = ResolvePrincipal(t.Context(), conn, "org_two", principal)
	require.ErrorIs(t, err, ErrPrincipalNotFound)

	missing := urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString())
	_, err = ResolvePrincipal(t.Context(), conn, "org_one", missing)
	require.ErrorIs(t, err, ErrPrincipalNotFound)

	_, err = conn.Exec(t.Context(), `UPDATE agents SET deleted_at = clock_timestamp() WHERE id = $1`, agent.ID)
	require.NoError(t, err)
	_, err = ResolvePrincipal(t.Context(), conn, "org_one", principal)
	require.ErrorIs(t, err, ErrPrincipalNotFound)

	malformed := urn.NewPrincipal(urn.PrincipalTypeAgent, "not-a-uuid")
	_, err = ResolvePrincipal(t.Context(), conn, "org_one", malformed)
	require.ErrorIs(t, err, ErrPrincipalInvalid)

	_, err = ResolvePrincipal(t.Context(), conn, "org_one", urn.NewPrincipal(urn.PrincipalTypeUser, "user_one"))
	require.ErrorIs(t, err, ErrPrincipalInvalid)

	conn.Close()
	_, err = ResolvePrincipal(t.Context(), conn, "org_one", missing)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrPrincipalInvalid))
	require.False(t, errors.Is(err, ErrPrincipalNotFound))
}
