package upstreamclients_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
)

func TestCheckConfiguredChoice(t *testing.T) {
	t.Parallel()

	shared, personal := uuid.New(), uuid.New()
	server := uuid.New()
	// Rows as ListServerConsentAuthClients returns them: a server offering
	// enabled clients needs upstream authentication, and one offering none
	// has a single row with no client.
	offer := func(pending bool, clients map[uuid.UUID]string) []repo.ListServerConsentAuthClientsRow {
		if len(clients) == 0 {
			return []repo.ListServerConsentAuthClientsRow{{McpServerID: server, UpstreamAuthPending: pending}}
		}
		rows := make([]repo.ListServerConsentAuthClientsRow, 0, len(clients))
		for id, clientType := range clients {
			rows = append(rows, repo.ListServerConsentAuthClientsRow{McpServerID: server, RequiresUpstreamAuth: true, AuthClientID: uuid.NullUUID{UUID: id, Valid: true}, ClientType: pgtype.Text{String: clientType, Valid: true}})
		}
		return rows
	}
	none := uuid.NullUUID{}
	pick := func(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: true} }
	clients := map[uuid.UUID]string{shared: authz.AuthClientTypeStaticCredential, personal: authz.AuthClientTypeRemoteSession}

	require.ErrorIs(t, upstreamclients.CheckConfiguredChoice(nil, none, true), upstreamclients.ErrChoiceNotOffered, "a server that offers nothing is not the organization's")
	require.NoError(t, upstreamclients.CheckConfiguredChoice(offer(false, nil), none, true), "a server offering no enabled client takes no-auth")
	require.ErrorIs(t, upstreamclients.CheckConfiguredChoice(offer(false, nil), pick(shared), true), upstreamclients.ErrChoiceNeedsNoCredential)
	require.ErrorIs(t, upstreamclients.CheckConfiguredChoice(offer(true, nil), none, true), upstreamclients.ErrChoiceUpstreamAuthPending, "credentials no auth client records yet are not a server needing none")
	require.ErrorIs(t, upstreamclients.CheckConfiguredChoice(offer(false, clients), none, true), upstreamclients.ErrChoiceNeedsCredential)
	require.NoError(t, upstreamclients.CheckConfiguredChoice(offer(false, clients), pick(shared), true))
	require.ErrorIs(t, upstreamclients.CheckConfiguredChoice(offer(false, clients), pick(personal), true), upstreamclients.ErrChoicePersonal)
	require.NoError(t, upstreamclients.CheckConfiguredChoice(offer(false, clients), pick(personal), false))
	require.ErrorIs(t, upstreamclients.CheckConfiguredChoice(offer(false, clients), pick(uuid.New()), false), upstreamclients.ErrChoiceNotOffered)
}
