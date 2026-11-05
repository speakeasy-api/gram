package remotesessions

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Who owns an associated client, as the management API reports it.
const (
	serverClientScopeProject      = "project"
	serverClientScopeOrganization = "organization"
	serverClientScopeGlobal       = "global"
)

func (s *Service) ListServerClientAssociations(ctx context.Context, payload *gen.ListServerClientAssociationsPayload) (*gen.ListServerClientAssociationsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").LogWarn(ctx, logger)
	}
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, serverID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}
	if _, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "MCP server not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get MCP server").LogError(ctx, logger)
	}

	rows, err := upstreamclientsrepo.New(s.db).ListMCPServerRemoteSessionClients(ctx, upstreamclientsrepo.ListMCPServerRemoteSessionClientsParams{
		McpServerID: serverID,
		ProjectID:   *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list MCP server client associations").LogError(ctx, logger)
	}

	associations := make([]*gen.ServerClientAssociation, 0, len(rows))
	for _, row := range rows {
		scope := serverClientScopeGlobal
		switch {
		case row.ClientProjectID.Valid:
			scope = serverClientScopeProject
		case row.ClientOrganizationID.Valid:
			scope = serverClientScopeOrganization
		}
		associations = append(associations, &gen.ServerClientAssociation{
			RemoteSessionClientID: row.RemoteSessionClientID.String(),
			ClientID:              row.ExternalClientID,
			RemoteSessionIssuerID: row.RemoteSessionIssuerID.String(),
			IssuerName:            conv.FromPGText[string](row.IssuerName),
			IssuerSlug:            row.IssuerSlug,
			IssuerURL:             row.IssuerUrl,
			Scope:                 scope,
			ServerCount:           row.ServerCount,
			AssociatedAt:          row.AssociatedAt.Time.Format(time.RFC3339),
		})
	}
	return &gen.ListServerClientAssociationsResult{Associations: associations}, nil
}

func (s *Service) DetachServerClientAssociation(ctx context.Context, payload *gen.DetachServerClientAssociationPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").LogWarn(ctx, logger)
	}
	clientID, err := uuid.Parse(payload.RemoteSessionClientID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid remote_session_client_id").LogWarn(ctx, logger)
	}
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, serverID.String(), authCtx.ProjectID.String())); err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := authclientbackfill.LockEMAWriterScope(ctx, dbtx, authCtx.ActiveOrganizationID, []uuid.UUID{*authCtx.ProjectID}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock association writer scope").LogError(ctx, logger)
	}
	// The typed synchronization below gates the registrations this server
	// reaches. Taking those gates before the server row, rather than when the
	// synchronization asks for them, keeps this in step with the paths that
	// reach the same gates without holding a server row.
	if err := authclientbackfill.PreflightServerAssociations(ctx, dbtx, authCtx.ActiveOrganizationID, serverID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock mcp server auth client scope").LogError(ctx, logger)
	}

	// The server lock serializes this with identity commits and server
	// updates, which change the bindings the sole-consumer check reads.
	server, err := mcpserversrepo.New(dbtx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "MCP server not found").LogWarn(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lock MCP server").LogError(ctx, logger)
	}

	q := upstreamclientsrepo.New(dbtx)
	soleConsumer, err := q.IsSoleConsumerBinding(ctx, upstreamclientsrepo.IsSoleConsumerBindingParams{
		RemoteSessionClientID: clientID,
		McpServerID:           serverID,
		ProjectID:             *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check MCP server client binding").LogError(ctx, logger)
	}
	// The sign-in's binding implies a sole consumer's association, so the
	// association cannot be removed on its own. Where typed auth clients
	// choose upstream authentication the server offers the client through its
	// link, which can: the detach then retires the link and leaves the
	// association for the upstream client choice to read again.
	if soleConsumer {
		enforced, err := authenforcement.ReadEnforced(ctx, dbtx, authCtx.ActiveOrganizationID)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "read upstream auth enforcement").LogError(ctx, logger)
		}
		if !enforced {
			return oops.E(oops.CodeConflict, nil, "this client is the server's own sign-in; turn off User Identity on the server to remove it").LogWarn(ctx, logger)
		}
	}

	rows, err := q.ListMCPServerRemoteSessionClients(ctx, upstreamclientsrepo.ListMCPServerRemoteSessionClientsParams{
		McpServerID: serverID,
		ProjectID:   *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list MCP server client associations").LogError(ctx, logger)
	}
	var externalClientID string
	found := false
	for _, row := range rows {
		if row.RemoteSessionClientID == clientID {
			externalClientID, found = row.ExternalClientID, true
			break
		}
	}
	if !found {
		return oops.E(oops.CodeNotFound, nil, "MCP server is not associated with this client").LogWarn(ctx, logger)
	}

	if soleConsumer {
		// The link is retired as an administrator's detach, which the
		// synchronization never undoes while the association it was derived
		// from stays. A client an administrator created is not this
		// association's and is left offered.
		aq := authclientsrepo.New(dbtx)
		offered, err := aq.ListServerAuthClients(ctx, authclientsrepo.ListServerAuthClientsParams{
			McpServerID:    serverID,
			ProjectID:      *authCtx.ProjectID,
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "list MCP server auth clients").LogError(ctx, logger)
		}
		retired := false
		for _, client := range offered {
			if client.Source != authclientbackfill.SourceBackfill || client.RemoteSessionClientID.UUID != clientID {
				continue
			}
			detached, err := aq.DetachAuthClientFromServer(ctx, authclientsrepo.DetachAuthClientFromServerParams{
				AuthClientID:   client.ID,
				McpServerID:    serverID,
				OrganizationID: authCtx.ActiveOrganizationID,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				// A concurrent detach already retired this link.
				continue
			}
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "detach MCP server auth client").LogError(ctx, logger)
			}
			if err := s.auditLogger.LogAuthClientDetachMcpServer(ctx, dbtx, audit.LogAuthClientMcpServerEvent{
				OrganizationID:   authCtx.ActiveOrganizationID,
				ProjectID:        detached.ProjectID,
				Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
				ActorDisplayName: authCtx.Email,
				ActorSlug:        nil,
				AuthClientURN:    urn.NewAuthClient(client.ID),
				AuthClientName:   client.Name,
				McpServerURN:     urn.NewMcpServer(serverID),
				McpServerName:    detached.McpServerName.String,
			}); err != nil {
				return oops.E(oops.CodeUnexpected, err, "log MCP server auth client detach").LogError(ctx, logger)
			}
			retired = true
		}
		if !retired {
			return oops.E(oops.CodeNotFound, nil, "MCP server has no auth client from this association left to detach").LogWarn(ctx, logger)
		}
	} else {
		if _, err := q.DetachMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.DetachMCPServerRemoteSessionClientParams{
			McpServerID:           serverID,
			RemoteSessionClientID: clientID,
			ProjectID:             *authCtx.ProjectID,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "detach MCP server client association").LogError(ctx, logger)
		}

		// The association is what the server's link to its typed auth client was
		// derived from, so deciding again here keeps the two models in step: the
		// link goes with the association, and the credential headers the client
		// copied from the source this server sat on are no longer its to send.
		// The client row itself is left alone — only the offline backfill retires
		// one no server reaches. Deciding from the same evidence is how a repeated
		// detach stays a no-op.
		if err := authclientbackfill.SyncServerAssociations(ctx, dbtx, authCtx.ActiveOrganizationID, serverID); err != nil {
			return oops.E(oops.CodeUnexpected, err, "sync mcp server auth clients").LogError(ctx, logger)
		}

		if err := s.auditLogger.LogRemoteSessionClientDetachMcpServer(ctx, dbtx, audit.LogRemoteSessionClientDetachMcpServerEvent{
			OrganizationID:         authCtx.ActiveOrganizationID,
			ProjectID:              *authCtx.ProjectID,
			Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:       authCtx.Email,
			ActorSlug:              nil,
			RemoteSessionClientURN: urn.NewRemoteSessionClient(clientID),
			ClientID:               externalClientID,
			McpServerURN:           urn.NewMcpServer(server.ID),
			McpServerName:          conv.FromPGTextOrEmpty[string](server.Name),
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log MCP server client detach").LogError(ctx, logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	return nil
}
