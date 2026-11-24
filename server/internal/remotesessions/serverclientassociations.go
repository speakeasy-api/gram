package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/remote_sessions"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authclients"
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
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
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
	if err != nil || serverID == uuid.Nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").LogWarn(ctx, logger)
	}
	if err := s.authz.Require(ctx, authz.MCPServer(authz.MCPServerIDOf(serverID), authz.ProjectIDOf(*authCtx.ProjectID)).Read()); err != nil {
		return nil, err
	}
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "MCP server not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get MCP server").LogError(ctx, logger)
	}

	requiredClientIDs := []string{}
	if server.UserSessionIssuerID.Valid {
		q := repo.New(s.db)
		bound, err := q.ListRemoteSessionClientsForUserSessionIssuer(ctx, repo.ListRemoteSessionClientsForUserSessionIssuerParams{
			UserSessionIssuerID: server.UserSessionIssuerID.UUID,
			ProjectID:           conv.ToNullUUID(*authCtx.ProjectID),
			OrganizationID:      conv.ToPGText(authCtx.ActiveOrganizationID),
			OrganizationWide:    false,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list clients bound to user session issuer").LogError(ctx, logger)
		}
		ids := make([]uuid.UUID, 0, len(bound))
		for _, client := range bound {
			ids = append(ids, client.ClientID)
		}
		required, _, err := requiredServerClients(ctx, q, authCtx.ActiveOrganizationID, server.UserSessionIssuerID.UUID, serverID, ids)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "decide required MCP server clients").LogError(ctx, logger)
		}
		requiredClientIDs = uuidStrings(required)
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
		associations = append(associations, serverClientAssociationView(row))
	}
	return &gen.ListServerClientAssociationsResult{Associations: associations, RequiredClientIds: requiredClientIDs}, nil
}

func serverClientAssociationView(row upstreamclientsrepo.ListMCPServerRemoteSessionClientsRow) *gen.ServerClientAssociation {
	scope := serverClientScopeGlobal
	switch {
	case row.ClientProjectID.Valid:
		scope = serverClientScopeProject
	case row.ClientOrganizationID.Valid:
		scope = serverClientScopeOrganization
	}
	return &gen.ServerClientAssociation{
		RemoteSessionClientID: row.RemoteSessionClientID.String(),
		ClientID:              row.ExternalClientID,
		RemoteSessionIssuerID: row.RemoteSessionIssuerID.String(),
		IssuerName:            conv.FromPGText[string](row.IssuerName),
		IssuerSlug:            row.IssuerSlug,
		IssuerURL:             row.IssuerUrl,
		Scope:                 scope,
		ServerCount:           row.ServerCount,
		AssociatedAt:          row.AssociatedAt.Time.Format(time.RFC3339),
	}
}

// serverClientAttachAttempts bounds how often an attach restarts its transaction
// when typed synchronization reports a concurrent change. Such a change is rare
// and short, so a third failure is reported for the caller to retry later.
const serverClientAttachAttempts = 3

func (s *Service) AttachServerClientAssociation(ctx context.Context, payload *gen.AttachServerClientAssociationPayload) (*gen.AttachServerClientAssociationResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil || serverID == uuid.Nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").LogWarn(ctx, logger)
	}
	clientID, err := uuid.Parse(payload.RemoteSessionClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client_id").LogWarn(ctx, logger)
	}
	if err := s.authz.Require(ctx, authz.MCPServer(authz.MCPServerIDOf(serverID), authz.ProjectIDOf(*authCtx.ProjectID)).Write()); err != nil {
		return nil, err
	}

	for attempt := 1; ; attempt++ {
		result, err := s.attachServerClient(ctx, logger, authCtx, serverID, clientID)
		if err == nil {
			return result, nil
		}
		if !authclientbackfill.IsRetryableSync(err) || attempt == serverClientAttachAttempts {
			return nil, err
		}
	}
}

// attachServerClient runs one attempt of an attach in its own transaction.
func (s *Service) attachServerClient(ctx context.Context, logger *slog.Logger, authCtx *contextvalues.AuthContext, serverID, clientID uuid.UUID) (*gen.AttachServerClientAssociationResult, error) {
	projectID := *authCtx.ProjectID
	organizationID := authCtx.ActiveOrganizationID

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	if err := authclientbackfill.LockEMAWriterScope(ctx, dbtx, organizationID, []uuid.UUID{projectID}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock association writer scope").LogError(ctx, logger)
	}

	// Resolving the client first keeps an id from another tenant from taking a
	// registration gate. Global clients are out of reach here: nothing can bind
	// one to a gateway's issuer on the way to serving it.
	sessions := repo.New(dbtx)
	found, err := sessions.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ID:             clientID,
		ProjectID:      projectID,
		OrganizationID: organizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote session client not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").LogError(ctx, logger)
	}
	client := found.RemoteSessionClient

	// Gate the new client's registration with the server's own, in one ordered
	// pass, before the row writes that follow: the sync can't find this client
	// until the association exists, which would be too late to take its gate in
	// order.
	if err := authclientbackfill.PreflightServerClientAttach(ctx, dbtx, organizationID, serverID, clientID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock mcp server auth client scope").LogError(ctx, logger)
	}
	server, err := mcpserversrepo.New(dbtx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "MCP server not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock MCP server").LogError(ctx, logger)
	}

	associations := upstreamclientsrepo.New(dbtx)
	current, err := associations.ListMCPServerRemoteSessionClients(ctx, upstreamclientsrepo.ListMCPServerRemoteSessionClientsParams{
		McpServerID: serverID,
		ProjectID:   projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list MCP server client associations").LogError(ctx, logger)
	}
	alreadyAssociated := slices.ContainsFunc(current, func(row upstreamclientsrepo.ListMCPServerRemoteSessionClientsRow) bool {
		return row.RemoteSessionClientID == clientID
	})
	if !alreadyAssociated {
		if len(current) > 0 {
			return nil, oops.E(oops.CodeConflict, nil, "this MCP server already has an upstream client; detach it before attaching another").LogWarn(ctx, logger)
		}
		if err := refuseClientAtAnotherAddress(ctx, logger, dbtx, organizationID, server, client); err != nil {
			return nil, err
		}
	}

	if !alreadyAssociated {
		associated, err := associations.AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{
			OrganizationID:        organizationID,
			RemoteSessionClientID: clientID,
			McpServerID:           serverID,
			ProjectID:             projectID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "associate MCP server with client").LogError(ctx, logger)
		}
		if associated == 0 {
			return nil, oops.E(oops.CodeConflict, nil, "the MCP server or its client changed while saving; retry").LogWarn(ctx, logger)
		}
	}

	if server.ToolsetID.Valid {
		if err := associations.RequireHostedUpstreamAssignment(ctx, upstreamclientsrepo.RequireHostedUpstreamAssignmentParams{ServerID: serverID, ProjectID: projectID}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "record hosted upstream requirement").LogError(ctx, logger)
		}
	}

	// Where typed auth clients choose upstream authentication, an explicit
	// attach is the undo for a detach that retired the server's link to the
	// client's backfilled auth client, which the synchronization otherwise
	// never relinks. That retirement looks like an administrator's own typed
	// detach, so with enforcement off, where the link changes no serving, the
	// administrator's detach is kept.
	authClients := authclientsrepo.New(dbtx)
	enforced, err := authenforcement.ReadEnforced(ctx, dbtx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read upstream auth enforcement").LogError(ctx, logger)
	}
	undone := []uuid.UUID{}
	if enforced {
		undone, err = authClients.UndoServerBackfillAuthClientDetaches(ctx, authclientsrepo.UndoServerBackfillAuthClientDetachesParams{
			McpServerID:           serverID,
			ProjectID:             projectID,
			OrganizationID:        organizationID,
			RemoteSessionClientID: uuid.NullUUID{UUID: clientID, Valid: true},
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "undo MCP server auth client detach").LogError(ctx, logger)
		}
	}

	if !alreadyAssociated || len(undone) > 0 {
		if err := authclientbackfill.SyncServerAssociations(ctx, dbtx, organizationID, serverID); err != nil {
			if authclientbackfill.IsRetryableSync(err) {
				return nil, oops.E(oops.CodeUnavailable, err, "this MCP server's clients changed while saving; retry").LogWarn(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "sync mcp server auth clients").LogError(ctx, logger)
		}
	}

	if len(undone) > 0 {
		relinked, err := authClients.ListServerBackfillAuthClients(ctx, authclientsrepo.ListServerBackfillAuthClientsParams{
			McpServerID:    serverID,
			ProjectID:      projectID,
			OrganizationID: organizationID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list MCP server auth clients").LogError(ctx, logger)
		}
		for _, authClient := range relinked {
			if !slices.Contains(undone, authClient.ID) {
				continue
			}
			if err := s.auditLogger.LogAuthClientAttachMcpServer(ctx, dbtx, audit.LogAuthClientMcpServerEvent{
				OrganizationID:   organizationID,
				ProjectID:        projectID,
				Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
				ActorDisplayName: authCtx.Email,
				ActorSlug:        nil,
				AuthClientURN:    urn.NewAuthClient(authClient.ID),
				AuthClientName:   authClient.Name,
				McpServerURN:     urn.NewMcpServer(server.ID),
				McpServerName:    conv.FromPGTextOrEmpty[string](server.Name),
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "log MCP server auth client attach").LogError(ctx, logger)
			}
		}
	}

	if !alreadyAssociated {
		// The server's upstream issuer is derived from its associations, and the
		// writer scope taken above covers this update.
		if server.UserSessionIssuerID.Valid {
			if _, err := mcpserversrepo.New(dbtx).ResyncMCPServerRemoteSessionIssuers(ctx, mcpserversrepo.ResyncMCPServerRemoteSessionIssuersParams{
				UserSessionIssuerIds: []uuid.UUID{server.UserSessionIssuerID.UUID},
				OrganizationID:       organizationID,
				ProjectID:            projectID,
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "resync mcp server upstream issuer").LogError(ctx, logger)
			}
		}

		if err := s.auditLogger.LogRemoteSessionClientAttachMcpServer(ctx, dbtx, audit.LogRemoteSessionClientAttachMcpServerEvent{
			OrganizationID:         organizationID,
			ProjectID:              projectID,
			Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:       authCtx.Email,
			ActorSlug:              nil,
			RemoteSessionClientURN: urn.NewRemoteSessionClient(clientID),
			ClientID:               client.ClientID,
			McpServerURN:           urn.NewMcpServer(server.ID),
			McpServerName:          conv.FromPGTextOrEmpty[string](server.Name),
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log MCP server client attach").LogError(ctx, logger)
		}
	}

	rows, err := associations.ListMCPServerRemoteSessionClients(ctx, upstreamclientsrepo.ListMCPServerRemoteSessionClientsParams{
		McpServerID: serverID,
		ProjectID:   projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list MCP server client associations").LogError(ctx, logger)
	}
	index := slices.IndexFunc(rows, func(row upstreamclientsrepo.ListMCPServerRemoteSessionClientsRow) bool {
		return row.RemoteSessionClientID == clientID
	})
	if index < 0 {
		return nil, oops.E(oops.CodeInvariantViolation, nil, "attached client is not associated").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	return &gen.AttachServerClientAssociationResult{
		Association:               serverClientAssociationView(rows[index]),
		BoundUserSessionIssuerIds: []string{},
	}, nil
}

// refuseClientAtAnotherAddress refuses a per-user client another proxied server
// uses at a different upstream address. A person holds one grant per client and
// it records one resource, so the last server connected would work: another
// direct server would fail closed, and another gateway member would call its
// upstream with no credential. A hosted server routes by no address, and a self
// client is one credential with no person's grant behind it.
func refuseClientAtAnotherAddress(ctx context.Context, logger *slog.Logger, dbtx pgx.Tx, organizationID string, server mcpserversrepo.McpServer, client repo.RemoteSessionClient) error {
	sessions := repo.New(dbtx)
	upstreamURL, applies, err := perUserClientAddress(ctx, sessions, server, client)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get MCP server upstream address").LogError(ctx, logger)
	}
	if !applies {
		return nil
	}
	others, err := sessions.ListProxiedMCPServersUsingClientAtAnotherAddress(ctx, repo.ListProxiedMCPServersUsingClientAtAnotherAddressParams{
		OrganizationID:        organizationID,
		RemoteSessionClientID: client.ID,
		McpServerID:           server.ID,
		UpstreamUrl:           upstreamURL,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list MCP servers using client").LogError(ctx, logger)
	}
	if len(others) > 0 {
		return oops.E(oops.CodeConflict, nil, clientAtAnotherAddressMessage).LogWarn(ctx, logger)
	}
	return nil
}

// clientAtAnotherAddressMessage is what refusing a client in use at another
// upstream address reads, whether another server already uses it or would pick
// it up from a gateway binding.
const clientAtAnotherAddressMessage = "another MCP server already uses this client at a different upstream address, and a person's sign-in through one client serves only one address. Use a different client"

// perUserClientAddress returns the upstream address a server's sign-ins through
// a client have to name, and whether one applies: it doesn't for a non-proxied
// server or a self client. A tunneled server has no fixed address.
func perUserClientAddress(ctx context.Context, sessions *repo.Queries, server mcpserversrepo.McpServer, client repo.RemoteSessionClient) (string, bool, error) {
	proxied := server.RemoteMcpServerID.Valid || server.TunneledMcpServerID.Valid
	if !proxied || CredentialOwner(client.CredentialOwner) == CredentialOwnerSelf {
		return "", false, nil
	}
	if !server.RemoteMcpServerID.Valid {
		return "", true, nil
	}
	upstreamURL, err := sessions.GetRemoteURLForMcpServer(ctx, repo.GetRemoteURLForMcpServerParams{McpServerID: server.ID, ProjectID: server.ProjectID})
	if err != nil {
		return "", false, fmt.Errorf("get remote url for mcp server: %w", err)
	}
	return upstreamURL, true, nil
}

func (s *Service) DetachServerClientAssociation(ctx context.Context, payload *gen.DetachServerClientAssociationPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil || serverID == uuid.Nil {
		return oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").LogWarn(ctx, logger)
	}
	clientID, err := uuid.Parse(payload.RemoteSessionClientID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid remote_session_client_id").LogWarn(ctx, logger)
	}
	if err := s.authz.Require(ctx, authz.MCPServer(authz.MCPServerIDOf(serverID), authz.ProjectIDOf(*authCtx.ProjectID)).Write()); err != nil {
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
	// updates, which can change the server or its credential sources.
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
	// In the typed serving path, preserve the explicit detach of the derived
	// offer and audit it. Outside that path the association's tombstone is
	// sufficient, so an independent typed administrator detach stays distinct.
	enforced, err := authenforcement.ReadEnforced(ctx, dbtx, authCtx.ActiveOrganizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "read upstream auth enforcement").LogError(ctx, logger)
	}
	if enforced {
		// Retire only the typed links derived from this association. Keep an
		// administrator's independent links and record a durable explicit detach.
		offered, err := authclientsrepo.New(dbtx).ListServerBackfillAuthClients(ctx, authclientsrepo.ListServerBackfillAuthClientsParams{
			McpServerID: serverID, ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "list MCP server auth clients").LogError(ctx, logger)
		}
		for _, client := range offered {
			if client.RemoteSessionClientID.UUID != clientID {
				continue
			}
			if err := authclients.DetachMcpServer(ctx, dbtx, s.auditLogger, authCtx, client, serverID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return oops.E(oops.CodeUnexpected, err, "detach MCP server auth client").LogError(ctx, logger)
			}
		}
	}

	if _, err := q.DetachMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.DetachMCPServerRemoteSessionClientParams{
		McpServerID: serverID, RemoteSessionClientID: clientID, ProjectID: *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "detach MCP server client association").LogError(ctx, logger)
	}

	if server.ToolsetID.Valid && !server.RequiresUpstreamAuth.Valid {
		// The presence of the old placement required credentials. Removing it
		// must not also opt the server (including a gateway member) out.
		if err := q.RequireHostedUpstreamAssignment(ctx, upstreamclientsrepo.RequireHostedUpstreamAssignmentParams{ServerID: server.ID, ProjectID: server.ProjectID}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "preserve hosted upstream authentication requirement").LogError(ctx, logger)
		}
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

	// The server's upstream issuer is derived from its associations, and the
	// writer scope taken above covers this update.
	if server.UserSessionIssuerID.Valid {
		if _, err := mcpserversrepo.New(dbtx).ResyncMCPServerRemoteSessionIssuers(ctx, mcpserversrepo.ResyncMCPServerRemoteSessionIssuersParams{
			UserSessionIssuerIds: []uuid.UUID{server.UserSessionIssuerID.UUID},
			OrganizationID:       authCtx.ActiveOrganizationID,
			ProjectID:            *authCtx.ProjectID,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "resync mcp server upstream issuer").LogError(ctx, logger)
		}
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
