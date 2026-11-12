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
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").LogWarn(ctx, logger)
	}
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, serverID.String(), authCtx.ProjectID.String())); err != nil {
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
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").LogWarn(ctx, logger)
	}
	clientID, err := uuid.Parse(payload.RemoteSessionClientID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_session_client_id").LogWarn(ctx, logger)
	}
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, serverID.String(), authCtx.ProjectID.String())); err != nil {
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

	gateways, err := sessions.ListGatewaysFrontingMCPServer(ctx, repo.ListGatewaysFrontingMCPServerParams{
		OrganizationID: organizationID,
		McpServerID:    serverID,
		ProjectID:      projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list gateways fronting MCP server").LogError(ctx, logger)
	}
	missing, err := s.refuseUnservableAttach(ctx, logger, sessions, organizationID, server, clientID, gateways, alreadyAssociated)
	if err != nil {
		return nil, err
	}

	bound, err := s.bindClientToGatewayIssuers(ctx, logger, dbtx, sessions, authCtx, server, client, gateways, missing, alreadyAssociated)
	if err != nil {
		return nil, err
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

		if err := authclientbackfill.SyncServerAssociations(ctx, dbtx, organizationID, serverID); err != nil {
			if authclientbackfill.IsRetryableSync(err) {
				return nil, oops.E(oops.CodeUnavailable, err, "this MCP server's clients changed while saving; retry").LogWarn(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "sync mcp server auth clients").LogError(ctx, logger)
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
	if len(bound) > 0 {
		BestEffortResyncMCPServerRemoteSessionIssuers(ctx, logger, s.db, organizationID, projectID, bound)
	}
	return &gen.AttachServerClientAssociationResult{
		Association:               serverClientAssociationView(rows[index]),
		BoundUserSessionIssuerIds: uuidStrings(bound),
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

// refuseUnservableAttach refuses an attach that would leave the server unable to
// sign in through the client on one of its paths, and returns the issuers of the
// fronting gateways that do not hold the client yet.
//
// A first association replaces a no-auth choice on every path, so each path has
// to reach the client. A server's own issuer serves its direct endpoint, which
// offers only clients bound to that issuer. A server with none needs the client
// bound to an issuer that fronts it, or reconcile would delete the association.
// The organization gateway fronts every server, but binding a client to its
// issuer is shared by every project's servers, so that stays an explicit step.
// An existing association has already replaced those choices, so attaching it
// again refuses nothing here and only reports what its gateways lack.
func (s *Service) refuseUnservableAttach(ctx context.Context, logger *slog.Logger, sessions *repo.Queries, organizationID string, server mcpserversrepo.McpServer, clientID uuid.UUID, gateways []repo.ListGatewaysFrontingMCPServerRow, alreadyAssociated bool) ([]uuid.UUID, error) {
	isBound := func(issuerID uuid.UUID) (bool, error) {
		count, err := sessions.CountRemoteSessionClientUserSessionIssuerBindings(ctx, repo.CountRemoteSessionClientUserSessionIssuerBindingsParams{
			RemoteSessionClientID: clientID,
			UserSessionIssuerID:   issuerID,
		})
		if err != nil {
			return false, fmt.Errorf("count client bindings: %w", err)
		}
		return count > 0, nil
	}

	organizationIssuers, err := sessions.ListOnlyOrganizationUserSessionIssuer(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization user session issuer").LogError(ctx, logger)
	}
	reachable := false
	for _, issuerID := range organizationIssuers {
		bound, err := isBound(issuerID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check organization issuer binding").LogError(ctx, logger)
		}
		if !bound && !alreadyAssociated {
			return nil, oops.E(oops.CodeConflict, nil, "this client isn't bound to the organization's user session issuer, so the organization gateway couldn't sign in with it. Bind it to that issuer first").LogWarn(ctx, logger)
		}
		reachable = true
	}

	var missing []uuid.UUID
	for _, gateway := range gateways {
		if slices.Contains(missing, gateway.UserSessionIssuerID) {
			continue
		}
		bound, err := isBound(gateway.UserSessionIssuerID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check gateway issuer binding").LogError(ctx, logger)
		}
		if !bound {
			missing = append(missing, gateway.UserSessionIssuerID)
			continue
		}
		reachable = true
	}

	if alreadyAssociated {
		return missing, nil
	}
	if server.UserSessionIssuerID.Valid {
		bound, err := isBound(server.UserSessionIssuerID.UUID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check server issuer binding").LogError(ctx, logger)
		}
		if !bound {
			return nil, oops.E(oops.CodeConflict, nil, "this client isn't bound to the MCP server's user session issuer, so the server's direct endpoint couldn't sign in with it. Bind it to that issuer first").LogWarn(ctx, logger)
		}
		return missing, nil
	}
	if !reachable {
		return nil, oops.E(oops.CodeConflict, nil, "this client isn't bound to any user session issuer that reaches the MCP server. Bind it to the issuer of a gateway fronting the server first").LogWarn(ctx, logger)
	}
	return missing, nil
}

// bindClientToGatewayIssuers binds the client to the issuers of the gateways
// fronting a server, as joining a gateway does for a server already associated,
// so the gateway's consent can offer the client. Each binding is refused when
// the caller can't write a gateway on the issuer, when it changes an
// organization-wide binding the caller can't change, when another client of the
// provider holds the issuer, which keeps one client per provider, or when
// reconcile would then associate another member at a different address. Attaching
// an existing association again skips an issuer in those last two cases rather
// than refusing, since the association already stands.
func (s *Service) bindClientToGatewayIssuers(ctx context.Context, logger *slog.Logger, dbtx pgx.Tx, sessions *repo.Queries, authCtx *contextvalues.AuthContext, server mcpserversrepo.McpServer, client repo.RemoteSessionClient, gateways []repo.ListGatewaysFrontingMCPServerRow, issuerIDs []uuid.UUID, alreadyAssociated bool) ([]uuid.UUID, error) {
	if len(issuerIDs) == 0 {
		return []uuid.UUID{}, nil
	}
	projectID := *authCtx.ProjectID
	organizationID := authCtx.ActiveOrganizationID

	for _, gateway := range gateways {
		if !slices.Contains(issuerIDs, gateway.UserSessionIssuerID) {
			continue
		}
		if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, gateway.MetaMcpServerID.String(), projectID.String())); err != nil {
			return nil, err
		}
	}

	// Issuer rows before the provider's binding lock, the order every other
	// client-binding writer takes them in.
	if err := lockUserSessionIssuersForClientBinding(ctx, logger, dbtx, sessions, projectID, organizationID, issuerIDs); err != nil {
		return nil, err
	}
	for _, issuerID := range issuerIDs {
		if err := s.refuseOrgWideAttachment(ctx, logger, sessions, *authCtx, issuerID, client.ID, true); err != nil {
			return nil, err
		}
	}
	if err := sessions.LockRemoteSessionIssuerForClientBinding(ctx, client.RemoteSessionIssuerID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock remote session issuer for client binding").LogError(ctx, logger)
	}
	upstreamURL, perUserAddress, err := perUserClientAddress(ctx, sessions, server, client)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get MCP server upstream address").LogError(ctx, logger)
	}

	bound := make([]uuid.UUID, 0, len(issuerIDs))
	for _, issuerID := range issuerIDs {
		others, err := sessions.ListOtherProviderClientsBoundToUserSessionIssuer(ctx, repo.ListOtherProviderClientsBoundToUserSessionIssuerParams{
			UserSessionIssuerID:   issuerID,
			RemoteSessionIssuerID: client.RemoteSessionIssuerID,
			RemoteSessionClientID: client.ID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list clients bound to gateway issuer").LogError(ctx, logger)
		}
		if len(others) > 0 {
			if alreadyAssociated {
				continue
			}
			return nil, oops.E(oops.CodeConflict, nil, "a gateway fronting this MCP server already signs in to this provider through another client, and each user session issuer keeps one client per provider").LogWarn(ctx, logger)
		}
		if perUserAddress {
			captured, err := sessions.ListGatewayMembersCaptureWouldAddAtAnotherAddress(ctx, repo.ListGatewayMembersCaptureWouldAddAtAnotherAddressParams{
				UserSessionIssuerID:   issuerID,
				OrganizationID:        organizationID,
				McpServerID:           server.ID,
				RemoteSessionIssuerID: client.RemoteSessionIssuerID,
				UpstreamUrl:           upstreamURL,
				RemoteSessionClientID: client.ID,
			})
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "list gateway members using provider").LogError(ctx, logger)
			}
			if len(captured) > 0 {
				if alreadyAssociated {
					continue
				}
				return nil, oops.E(oops.CodeConflict, nil, clientAtAnotherAddressMessage).LogWarn(ctx, logger)
			}
		}
		if err := sessions.AttachRemoteSessionClientToUserSessionIssuer(ctx, repo.AttachRemoteSessionClientToUserSessionIssuerParams{
			RemoteSessionClientID: client.ID,
			UserSessionIssuerID:   issuerID,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "bind client to gateway issuer").LogError(ctx, logger)
		}
		if err := s.auditLogger.LogRemoteSessionClientAttachUserSessionIssuer(ctx, dbtx, audit.LogRemoteSessionClientUserSessionIssuerAttachmentEvent{
			OrganizationID:         organizationID,
			ProjectID:              projectID,
			Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:       authCtx.Email,
			ActorSlug:              nil,
			RemoteSessionClientURN: urn.NewRemoteSessionClient(client.ID),
			ClientID:               client.ClientID,
			UserSessionIssuerURN:   urn.NewUserSessionIssuer(issuerID),
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log gateway issuer binding").LogError(ctx, logger)
		}
		bound = append(bound, issuerID)
	}
	return bound, nil
}

// releaseFrontingGatewayBindings unbinds a client from the issuers of the
// gateways fronting a server that has just given it up, undoing what an attach
// or joining the gateway bound there, so another client of the provider can
// take its place. A binding stays when anything else there may sign in through
// it, or when the caller couldn't have removed it directly. So does every one of
// them when the client isn't bound to the server's own issuer: an attach needs a
// binding to reach the server, so removing the last would leave the detach with
// no undo.
func (s *Service) releaseFrontingGatewayBindings(ctx context.Context, logger *slog.Logger, dbtx pgx.Tx, authCtx *contextvalues.AuthContext, server mcpserversrepo.McpServer, clientID uuid.UUID) ([]uuid.UUID, error) {
	projectID := *authCtx.ProjectID
	organizationID := authCtx.ActiveOrganizationID
	serverID := server.ID
	sessions := repo.New(dbtx)

	if !server.UserSessionIssuerID.Valid {
		return nil, nil
	}
	ownBindings, err := sessions.CountRemoteSessionClientUserSessionIssuerBindings(ctx, repo.CountRemoteSessionClientUserSessionIssuerBindingsParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   server.UserSessionIssuerID.UUID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check server issuer binding").LogError(ctx, logger)
	}
	if ownBindings == 0 {
		return nil, nil
	}

	gateways, err := sessions.ListGatewaysFrontingMCPServer(ctx, repo.ListGatewaysFrontingMCPServerParams{
		OrganizationID: organizationID,
		McpServerID:    serverID,
		ProjectID:      projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list gateways fronting MCP server").LogError(ctx, logger)
	}
	writable := map[uuid.UUID]bool{}
	var issuerIDs []uuid.UUID
	for _, gateway := range gateways {
		allowed, err := s.authz.Evaluate(ctx, authz.MCPCheck(authz.ScopeMCPWrite, gateway.MetaMcpServerID.String(), projectID.String()))
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check gateway write access").LogError(ctx, logger)
		}
		if _, seen := writable[gateway.UserSessionIssuerID]; !seen {
			writable[gateway.UserSessionIssuerID] = true
			issuerIDs = append(issuerIDs, gateway.UserSessionIssuerID)
		}
		writable[gateway.UserSessionIssuerID] = writable[gateway.UserSessionIssuerID] && allowed
	}
	if len(issuerIDs) == 0 {
		return nil, nil
	}

	if err := lockUserSessionIssuersForClientBinding(ctx, logger, dbtx, sessions, projectID, organizationID, issuerIDs); err != nil {
		return nil, err
	}
	found, err := sessions.GetRemoteSessionClientByID(ctx, repo.GetRemoteSessionClientByIDParams{
		ID:             clientID,
		ProjectID:      projectID,
		OrganizationID: organizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// A global client is bound by the platform, not through this path.
		return nil, nil
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get remote session client").LogError(ctx, logger)
	}
	client := found.RemoteSessionClient
	if err := sessions.LockRemoteSessionIssuerForClientBinding(ctx, client.RemoteSessionIssuerID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock remote session issuer for client binding").LogError(ctx, logger)
	}

	var released []uuid.UUID
	for _, issuerID := range issuerIDs {
		if !writable[issuerID] {
			continue
		}
		inUse, err := gatewayIssuerStillUsesClient(ctx, sessions, organizationID, projectID, serverID, client, issuerID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check gateway issuer consumers").LogError(ctx, logger)
		}
		if inUse {
			continue
		}
		removed, err := sessions.DetachProjectRemoteSessionClientFromUserSessionIssuer(ctx, repo.DetachProjectRemoteSessionClientFromUserSessionIssuerParams{
			ProjectID:             projectID,
			OrganizationID:        organizationID,
			RemoteSessionClientID: clientID,
			UserSessionIssuerID:   issuerID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "unbind client from gateway issuer").LogError(ctx, logger)
		}
		if removed == 0 {
			continue
		}
		if err := s.auditLogger.LogRemoteSessionClientDetachUserSessionIssuer(ctx, dbtx, audit.LogRemoteSessionClientUserSessionIssuerAttachmentEvent{
			OrganizationID:         organizationID,
			ProjectID:              projectID,
			Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:       authCtx.Email,
			ActorSlug:              nil,
			RemoteSessionClientURN: urn.NewRemoteSessionClient(clientID),
			ClientID:               client.ClientID,
			UserSessionIssuerURN:   urn.NewUserSessionIssuer(issuerID),
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log gateway issuer unbinding").LogError(ctx, logger)
		}
		released = append(released, issuerID)
	}
	return released, nil
}

// gatewayIssuerStillUsesClient reports whether a client's binding to a gateway's
// issuer has to stay after serverID gave the client up: another server is
// associated with it through the issuer, something on the issuer signs in
// without an association, an agent attachment goes through the binding, or the
// issuer is organization-level, whose bindings every project's servers share,
// the organization gateway's hosted members among them.
func gatewayIssuerStillUsesClient(ctx context.Context, sessions *repo.Queries, organizationID string, projectID, serverID uuid.UUID, client repo.RemoteSessionClient, issuerID uuid.UUID) (bool, error) {
	issuer, err := sessions.GetUserSessionIssuerForProject(ctx, repo.GetUserSessionIssuerForProjectParams{
		ID:             issuerID,
		ProjectID:      projectID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return false, fmt.Errorf("get user session issuer: %w", err)
	}
	if !issuer.ProjectID.Valid {
		return true, nil
	}
	uses, err := sessions.DescribeBoundClientUse(ctx, repo.DescribeBoundClientUseParams{
		McpServerID:            serverID,
		OrganizationID:         organizationID,
		UserSessionIssuerID:    issuerID,
		RemoteSessionClientIds: []uuid.UUID{client.ID},
	})
	if err != nil {
		return false, fmt.Errorf("describe bound client use: %w", err)
	}
	if len(uses) != 1 || len(uses[0].OtherMcpServerIds) > 0 {
		return true, nil
	}
	consumers, err := sessions.UserSessionIssuerHasConsumersWithoutAssociations(ctx, repo.UserSessionIssuerHasConsumersWithoutAssociationsParams{
		OrganizationID:      organizationID,
		UserSessionIssuerID: issuerID,
		McpServerID:         serverID,
	})
	if err != nil {
		return false, fmt.Errorf("check user session issuer consumers: %w", err)
	}
	if consumers {
		return true, nil
	}
	attached, err := sessions.HasLivePrincipalRemoteSessionBindingsForClientBinding(ctx, repo.HasLivePrincipalRemoteSessionBindingsForClientBindingParams{
		OrganizationID:        organizationID,
		RemoteSessionClientID: client.ID,
		UserSessionIssuerID:   issuerID,
	})
	if err != nil {
		return false, fmt.Errorf("check agent attachments through client binding: %w", err)
	}
	if attached {
		return true, nil
	}
	// An organization client's binding can serve every project's bindings.
	emaProjectID := projectID
	if !client.ProjectID.Valid {
		emaProjectID = uuid.Nil
	}
	emaBindings, err := sessions.CountActiveEMABindingsForClientUserIssuer(ctx, repo.CountActiveEMABindingsForClientUserIssuerParams{
		ClientID:            conv.ToNullUUID(client.ID),
		UserSessionIssuerID: issuerID,
		OrganizationID:      organizationID,
		ProjectID:           emaProjectID,
	})
	if err != nil {
		return false, fmt.Errorf("count enterprise-managed bindings through client binding: %w", err)
	}
	return emaBindings > 0, nil
}

// errClientNotDetachable says a server has nothing to detach from a client: it
// has no association with it, and either the client is not one it falls back to
// needing or the detach is already on record.
var errClientNotDetachable = errors.New("client is not detachable from this mcp server")

// recordUnclaimedClientDetach records that a server gives up a client bound to
// its issuer that it has no association with, returning the client's external
// id. Only an unclaimed client qualifies, since that is the only kind the server
// could still need without an association.
func recordUnclaimedClientDetach(ctx context.Context, dbtx pgx.Tx, organizationID string, server mcpserversrepo.McpServer, clientID uuid.UUID) (string, error) {
	if !server.UserSessionIssuerID.Valid {
		return "", errClientNotDetachable
	}
	uses, err := repo.New(dbtx).DescribeBoundClientUse(ctx, repo.DescribeBoundClientUseParams{
		McpServerID:            server.ID,
		OrganizationID:         organizationID,
		UserSessionIssuerID:    server.UserSessionIssuerID.UUID,
		RemoteSessionClientIds: []uuid.UUID{clientID},
	})
	if err != nil {
		return "", fmt.Errorf("describe bound client use: %w", err)
	}
	if len(uses) != 1 || !clientUnclaimed(uses[0]) {
		return "", errClientNotDetachable
	}
	externalClientID, err := upstreamclientsrepo.New(dbtx).RecordMCPServerRemoteSessionClientDetach(ctx, upstreamclientsrepo.RecordMCPServerRemoteSessionClientDetachParams{
		OrganizationID:        organizationID,
		McpServerID:           server.ID,
		ProjectID:             server.ProjectID,
		RemoteSessionClientID: clientID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errClientNotDetachable
	}
	if err != nil {
		return "", fmt.Errorf("record mcp server client detach: %w", err)
	}
	return externalClientID, nil
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
	var releasedIssuers []uuid.UUID
	found := false
	for _, row := range rows {
		if row.RemoteSessionClientID == clientID {
			externalClientID, found = row.ExternalClientID, true
			break
		}
	}
	if soleConsumer && !found {
		return oops.E(oops.CodeNotFound, nil, "MCP server is not associated with this client").LogWarn(ctx, logger)
	}

	if soleConsumer {
		// The link is retired as an administrator's detach, which the
		// synchronization never undoes while the association it was derived
		// from stays. Administrator-owned links, including ones to backfilled
		// clients, are not this association's and stay offered.
		offered, err := authclientsrepo.New(dbtx).ListServerBackfillAuthClients(ctx, authclientsrepo.ListServerBackfillAuthClientsParams{
			McpServerID:    serverID,
			ProjectID:      *authCtx.ProjectID,
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "list MCP server auth clients").LogError(ctx, logger)
		}
		retired := false
		for _, client := range offered {
			if client.RemoteSessionClientID.UUID != clientID {
				continue
			}
			err := authclients.DetachMcpServer(ctx, dbtx, s.auditLogger, authCtx, client, serverID)
			if errors.Is(err, pgx.ErrNoRows) {
				// A concurrent detach already retired this link.
				continue
			}
			if err != nil {
				return oops.E(oops.CodeUnexpected, err, "detach MCP server auth client").LogError(ctx, logger)
			}
			retired = true
		}
		if !retired {
			return oops.E(oops.CodeNotFound, nil, "MCP server has no auth client from this association left to detach").LogWarn(ctx, logger)
		}
	} else {
		if found {
			if _, err := q.DetachMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.DetachMCPServerRemoteSessionClientParams{
				McpServerID:           serverID,
				RemoteSessionClientID: clientID,
				ProjectID:             *authCtx.ProjectID,
			}); err != nil {
				return oops.E(oops.CodeUnexpected, err, "detach MCP server client association").LogError(ctx, logger)
			}
			releasedIssuers, err = s.releaseFrontingGatewayBindings(ctx, logger, dbtx, authCtx, server, clientID)
			if err != nil {
				return err
			}
		} else {
			externalClientID, err = recordUnclaimedClientDetach(ctx, dbtx, authCtx.ActiveOrganizationID, server, clientID)
			switch {
			case errors.Is(err, errClientNotDetachable):
				return oops.E(oops.CodeNotFound, err, "MCP server is not associated with this client").LogWarn(ctx, logger)
			case err != nil:
				return oops.E(oops.CodeUnexpected, err, "record MCP server client detach").LogError(ctx, logger)
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
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}
	if len(releasedIssuers) > 0 {
		BestEffortResyncMCPServerRemoteSessionIssuers(ctx, logger, s.db, authCtx.ActiveOrganizationID, *authCtx.ProjectID, releasedIssuers)
	}
	return nil
}
