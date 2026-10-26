package keys

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// recordAgentKeyAuthClientChoices records the upstream credential an agent
// key uses on each MCP server it names. A choice must be a shared credential
// the server offers enabled, or no upstream authentication on a server that
// needs none. The choice grants nothing: every request still checks that the
// agent may use the credential and reach the server.
func recordAgentKeyAuthClientChoices(ctx context.Context, tx pgx.Tx, organizationID string, keyID uuid.UUID, choices []*gen.AgentKeyAuthClientChoice) error {
	if len(choices) == 0 {
		return nil
	}
	queries := upstreamclientsrepo.New(tx)
	seen := make(map[uuid.UUID]bool, len(choices))
	for _, choice := range choices {
		serverID, err := uuid.Parse(choice.McpServerID)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "invalid mcp server id")
		}
		if seen[serverID] {
			return oops.E(oops.CodeBadRequest, nil, "a server can have only one credential choice")
		}
		seen[serverID] = true

		clientID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
		if choice.AuthClientID != nil {
			parsed, err := uuid.Parse(*choice.AuthClientID)
			if err != nil {
				return oops.E(oops.CodeBadRequest, err, "invalid auth client id")
			}
			clientID = uuid.NullUUID{UUID: parsed, Valid: true}
		}

		offered, err := queries.ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: organizationID, McpServerIds: []uuid.UUID{serverID}})
		if err != nil {
			return fmt.Errorf("list server auth clients: %w", err)
		}
		if len(offered) == 0 {
			return oops.E(oops.CodeNotFound, nil, "mcp server not found")
		}
		if err := upstreamclients.CheckConfiguredChoice(offered, clientID, true); err != nil {
			if errors.Is(err, upstreamclients.ErrChoicePersonal) {
				return oops.E(oops.CodeBadRequest, err, "agent keys use shared credentials; an agent reaches a person's own connection only when it acts for them")
			}
			return oops.E(oops.CodeBadRequest, err, "%s", err.Error())
		}
		for _, row := range offered {
			if row.AuthClientID == clientID && clientID.Valid && !authz.IsAgentKeyAuthClientType(row.ClientType.String) {
				return oops.E(oops.CodeBadRequest, nil, "agent keys cannot use a pass-through credential; the agent gateway does not forward the caller's headers to its servers")
			}
		}

		recorded, err := queries.RecordAuthClientChoice(ctx, upstreamclientsrepo.RecordAuthClientChoiceParams{
			CredentialKind: upstreamclients.CredentialKindAPIKey,
			CredentialID:   keyID,
			Source:         upstreamclients.SelectionSourceIssuance,
			OrganizationID: organizationID,
			AuthClientID:   clientID,
			McpServerID:    serverID,
		})
		if err != nil {
			return fmt.Errorf("record agent key credential choice: %w", err)
		}
		if recorded != 1 {
			return oops.E(oops.CodeConflict, nil, "the server's credentials changed; review the choice and try again")
		}
	}
	return nil
}
