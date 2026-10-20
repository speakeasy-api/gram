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
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
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
	if soleConsumer {
		return oops.E(oops.CodeConflict, nil, "this client is the server's own sign-in; turn off User Identity on the server to remove it").LogWarn(ctx, logger)
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

	if _, err := q.DetachMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.DetachMCPServerRemoteSessionClientParams{
		McpServerID:           serverID,
		RemoteSessionClientID: clientID,
		ProjectID:             *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "detach MCP server client association").LogError(ctx, logger)
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

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	return nil
}
