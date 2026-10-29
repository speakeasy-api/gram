package keys

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID != organizationID {
		return oops.C(oops.CodeUnauthorized)
	}
	// Match issuance's live creator authority, not cached session roles or
	// the agent's policy. Recording a choice grants the agent no access.
	principals, err := authz.ResolveUserPrincipals(ctx, tx, organizationID, authCtx.UserID)
	if err != nil {
		return fmt.Errorf("resolve credential choice creator: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, tx, organizationID, principals)
	if err != nil {
		return fmt.Errorf("load credential choice creator policy: %w", err)
	}
	queries := upstreamclientsrepo.New(tx)
	seen := make(map[uuid.UUID]bool, len(choices))
	for _, choice := range choices {
		if choice == nil {
			return oops.E(oops.CodeBadRequest, nil, "credential choice must not be null")
		}
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

		server, err := authclientsrepo.New(tx).GetOrganizationMCPServer(ctx, authclientsrepo.GetOrganizationMCPServerParams{OrganizationID: organizationID, ID: serverID})
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp server not found")
		}
		if err != nil {
			return fmt.Errorf("load credential choice server: %w", err)
		}
		allowed, err := authz.GrantsAuthorize(grants, authz.MCPCheck(authz.ScopeMCPConnect, server.ID.String(), server.ProjectID.String()))
		if err != nil {
			return fmt.Errorf("authorize credential choice server: %w", err)
		}
		if !allowed {
			return oops.C(oops.CodeForbidden)
		}

		offered, err := queries.ListServerConsentAuthClients(ctx, upstreamclientsrepo.ListServerConsentAuthClientsParams{OrganizationID: organizationID, McpServerIds: []uuid.UUID{serverID}})
		if err != nil {
			return fmt.Errorf("list server auth clients: %w", err)
		}
		if len(offered) == 0 {
			return oops.E(oops.CodeNotFound, nil, "mcp server not found")
		}
		if err := checkAgentKeyChoice(offered, clientID); err != nil {
			return err
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

// checkAgentKeyChoice refuses a choice the server does not offer to agent
// keys. offered is the server's enabled clients, one row with no client when
// it offers none.
func checkAgentKeyChoice(offered []upstreamclientsrepo.ListServerConsentAuthClientsRow, clientID uuid.NullUUID) error {
	requires := offered[0].RequiresUpstreamAuth
	if !clientID.Valid {
		hasClients := offered[0].AuthClientID.Valid
		if (requires.Valid && requires.Bool) || (!requires.Valid && hasClients) {
			return oops.E(oops.CodeBadRequest, nil, "this server needs upstream credentials; choose one")
		}
		return nil
	}
	for _, row := range offered {
		if row.AuthClientID != clientID {
			continue
		}
		switch {
		case authz.IsAgentKeyAuthClientType(row.ClientType.String):
			return nil
		case row.ClientType.String == authz.AuthClientTypePassThrough:
			return oops.E(oops.CodeBadRequest, nil, "agent keys cannot use a pass-through credential; the agent gateway does not forward the caller's headers to its servers")
		}
		return oops.E(oops.CodeBadRequest, nil, "agent keys use shared credentials; an agent reaches a person's own connection only when it acts for them")
	}
	return oops.E(oops.CodeBadRequest, nil, "the server does not offer that credential")
}
