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
	offer := func(requires pgtype.Bool, clients map[uuid.UUID]string) []repo.ListServerConsentAuthClientsRow {
		if len(clients) == 0 {
			return []repo.ListServerConsentAuthClientsRow{{McpServerID: server, RequiresUpstreamAuth: requires}}
		}
		rows := make([]repo.ListServerConsentAuthClientsRow, 0, len(clients))
		for id, clientType := range clients {
			rows = append(rows, repo.ListServerConsentAuthClientsRow{McpServerID: server, RequiresUpstreamAuth: requires, AuthClientID: uuid.NullUUID{UUID: id, Valid: true}, ClientType: pgtype.Text{String: clientType, Valid: true}})
		}
		return rows
	}
	none := uuid.NullUUID{}
	pick := func(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: true} }
	needs := pgtype.Bool{Bool: true, Valid: true}
	needsNone := pgtype.Bool{Bool: false, Valid: true}
	undecided := pgtype.Bool{}
	clients := map[uuid.UUID]string{shared: authz.AuthClientTypeStaticCredential, personal: authz.AuthClientTypeRemoteSession}

	require.NoError(t, upstreamclients.CheckConfiguredChoice(offer(needsNone, nil), none, true), "a server needing none takes no-auth")
	require.ErrorIs(t, upstreamclients.CheckConfiguredChoice(offer(undecided, nil), none, true), upstreamclients.ErrChoiceUpstreamAuthUndecided)
	require.ErrorIs(t, upstreamclients.CheckConfiguredChoice(offer(needs, clients), none, true), upstreamclients.ErrChoiceNeedsCredential)
	require.ErrorIs(t, upstreamclients.CheckConfiguredChoice(offer(needsNone, clients), pick(shared), true), upstreamclients.ErrChoiceNeedsNoCredential)
	require.NoError(t, upstreamclients.CheckConfiguredChoice(offer(needs, clients), pick(shared), true))
	require.ErrorIs(t, upstreamclients.CheckConfiguredChoice(offer(needs, clients), pick(personal), true), upstreamclients.ErrChoicePersonal)
	require.NoError(t, upstreamclients.CheckConfiguredChoice(offer(needs, clients), pick(personal), false))
	require.ErrorIs(t, upstreamclients.CheckConfiguredChoice(offer(needs, clients), pick(uuid.New()), false), upstreamclients.ErrChoiceNotOffered)
}
