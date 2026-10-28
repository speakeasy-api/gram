// Package authclients manages the typed auth clients Speakeasy uses to
// authenticate to upstream MCP servers and the servers that offer them.
// Managing a client is an administrative act: it never grants permission to
// use the client, which is decided by mcp:auth grants alone.
package authclients

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/auth_clients"
	srv "github.com/speakeasy-api/gram/server/gen/http/auth_clients/server"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	StatusConfigured      = "configured"
	StatusSetupIncomplete = "setup_incomplete"
	StatusNoAuthRequired  = "no_auth_required"
	StatusUndecided       = "undecided"
)

type Service struct {
	enc        *encryption.Client
	githubApps githubAppVerifier
	tracer     trace.Tracer
	logger     *slog.Logger
	db         *pgxpool.Pool
	auth       *auth.Auth
	authz      *authz.Engine
	audit      *audit.Logger
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	options ...func(*Service),
) *Service {
	logger = logger.With(attr.SlogComponent("authclients"))

	s := &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/authclients"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
		audit:  auditLogger,
	}
	for _, option := range options {
		option(s)
	}
	return s
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) ListAuthClients(ctx context.Context, payload *gen.ListAuthClientsPayload) (*gen.ListAuthClientsResult, error) {
	authCtx, logger, err := s.require(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	serverID, err := parseOptionalUUID(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, logger)
	}
	cursor, err := parseOptionalUUID(payload.Cursor)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").LogError(ctx, logger)
	}
	limit := int32(min(payload.Limit, constants.MaxPageLimit)) //nolint:gosec // bounded by MaxPageLimit
	if limit <= 0 {
		limit = constants.DefaultPageLimit
	}

	queries := repo.New(s.db)
	clients, err := queries.ListAuthClients(ctx, repo.ListAuthClientsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ClientType:     conv.PtrToPGText(payload.ClientType),
		McpServerID:    serverID,
		Cursor:         cursor,
		LimitValue:     limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list auth clients").LogError(ctx, logger)
	}

	items, err := clientViews(ctx, queries, authCtx.ActiveOrganizationID, clients)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build auth client views").LogError(ctx, logger)
	}

	var nextCursor *string
	if len(clients) >= int(limit) {
		nextCursor = new(clients[len(clients)-1].ID.String())
	}

	return &gen.ListAuthClientsResult{Items: items, NextCursor: nextCursor}, nil
}

func (s *Service) GetAuthClient(ctx context.Context, payload *gen.GetAuthClientPayload) (*types.AuthClient, error) {
	authCtx, logger, err := s.require(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid auth client id").LogError(ctx, logger)
	}

	queries := repo.New(s.db)
	client, err := queries.GetAuthClient(ctx, repo.GetAuthClientParams{ID: clientID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "auth client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get auth client").LogError(ctx, logger)
	}

	return s.view(ctx, queries, authCtx.ActiveOrganizationID, client, logger)
}

func (s *Service) UpdateAuthClient(ctx context.Context, payload *gen.UpdateAuthClientPayload) (*types.AuthClient, error) {
	authCtx, logger, err := s.require(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid auth client id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	existing, err := queries.LockAuthClient(ctx, repo.LockAuthClientParams{ID: clientID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "auth client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock auth client").LogError(ctx, logger)
	}

	before, err := s.view(ctx, queries, authCtx.ActiveOrganizationID, existing, logger)
	if err != nil {
		return nil, err
	}

	name := conv.PtrValOr(payload.Name, existing.Name)
	enabled := conv.PtrValOr(payload.Enabled, existing.Enabled)
	if name == existing.Name && enabled == existing.Enabled {
		return before, nil
	}

	if enabled && !existing.Enabled {
		// Locking the offering servers serializes with
		// SetMcpServerUpstreamAuthRequirement, which locks the server before
		// checking for enabled clients.
		servers, err := queries.LockAuthClientServerRequirements(ctx, repo.LockAuthClientServerRequirementsParams{
			AuthClientID:   clientID,
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "lock auth client servers").LogError(ctx, logger)
		}
		for _, srv := range servers {
			if srv.RequiresUpstreamAuth.Valid && !srv.RequiresUpstreamAuth.Bool {
				return nil, oops.E(oops.CodeConflict, nil, "a server that offers this auth client declares that its upstream needs no authentication; change that declaration or detach the client before enabling it").LogError(ctx, logger)
			}
		}
	}

	updated, err := queries.UpdateAuthClient(ctx, repo.UpdateAuthClientParams{
		ID:             clientID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Name:           name,
		Enabled:        enabled,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "update auth client").LogError(ctx, logger)
	}

	after, err := s.view(ctx, queries, authCtx.ActiveOrganizationID, updated, logger)
	if err != nil {
		return nil, err
	}

	event := audit.LogAuthClientUpdateEvent{
		OrganizationID:           authCtx.ActiveOrganizationID,
		ProjectID:                updated.ProjectID.UUID,
		Actor:                    urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:         authCtx.Email,
		ActorSlug:                nil,
		AuthClientURN:            urn.NewAuthClient(updated.ID),
		AuthClientName:           updated.Name,
		AuthClientSnapshotBefore: before,
		AuthClientSnapshotAfter:  after,
	}
	if name != existing.Name {
		if err := s.audit.LogAuthClientUpdate(ctx, dbtx, event); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log auth client update").LogError(ctx, logger)
		}
	}
	if enabled != existing.Enabled {
		logEnabled := s.audit.LogAuthClientDisable
		if enabled {
			logEnabled = s.audit.LogAuthClientEnable
		}
		if err := logEnabled(ctx, dbtx, event); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log auth client enabled change").LogError(ctx, logger)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return after, nil
}

func (s *Service) DetachAuthClientFromMcpServer(ctx context.Context, payload *gen.DetachAuthClientFromMcpServerPayload) (*types.AuthClient, error) {
	authCtx, logger, err := s.require(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	clientID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid auth client id").LogError(ctx, logger)
	}
	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	client, err := queries.LockAuthClient(ctx, repo.LockAuthClientParams{ID: clientID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "auth client not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock auth client").LogError(ctx, logger)
	}

	detached, err := queries.DetachAuthClientFromServer(ctx, repo.DetachAuthClientFromServerParams{
		AuthClientID:   clientID,
		McpServerID:    serverID,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "the mcp server does not offer this auth client").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "detach auth client from mcp server").LogError(ctx, logger)
	}

	if err := s.audit.LogAuthClientDetachMcpServer(ctx, dbtx, audit.LogAuthClientDetachMcpServerEvent{
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
		return nil, oops.E(oops.CodeUnexpected, err, "log auth client detach").LogError(ctx, logger)
	}

	view, err := s.view(ctx, queries, authCtx.ActiveOrganizationID, client, logger)
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return view, nil
}

func (s *Service) GetMcpServerAuthentication(ctx context.Context, payload *gen.GetMcpServerAuthenticationPayload) (*types.McpServerAuthentication, error) {
	authCtx, logger, err := s.require(ctx, authz.ScopeOrgRead)
	if err != nil {
		return nil, err
	}

	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, logger)
	}

	queries := repo.New(s.db)
	server, err := queries.GetOrganizationMCPServer(ctx, repo.GetOrganizationMCPServerParams{ID: serverID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp server").LogError(ctx, logger)
	}

	return s.serverAuthentication(ctx, queries, authCtx.ActiveOrganizationID, server.ID, server.ProjectID, server.Visibility, server.RequiresUpstreamAuth, logger)
}

func (s *Service) SetMcpServerUpstreamAuthRequirement(ctx context.Context, payload *gen.SetMcpServerUpstreamAuthRequirementPayload) (*types.McpServerAuthentication, error) {
	authCtx, logger, err := s.require(ctx, authz.ScopeOrgAdmin)
	if err != nil {
		return nil, err
	}

	serverID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	server, err := queries.LockOrganizationMCPServer(ctx, repo.LockOrganizationMCPServerParams{ID: serverID, OrganizationID: authCtx.ActiveOrganizationID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock mcp server").LogError(ctx, logger)
	}
	if server.ToolsetID.Valid {
		return nil, oops.E(oops.CodeBadRequest, nil, "hosted toolsets keep their own authentication; their upstream requirement cannot be set").LogError(ctx, logger)
	}

	required := payload.RequiresUpstreamAuth
	if !required {
		clients, err := queries.ListServerAuthClients(ctx, repo.ListServerAuthClientsParams{
			McpServerID:    server.ID,
			ProjectID:      server.ProjectID,
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list server auth clients").LogError(ctx, logger)
		}
		for _, c := range clients {
			if c.Enabled {
				return nil, oops.E(oops.CodeConflict, nil, "the server still offers enabled auth clients; detach or disable them before declaring that its upstream needs no authentication").LogError(ctx, logger)
			}
		}
	}

	if !server.RequiresUpstreamAuth.Valid || server.RequiresUpstreamAuth.Bool != required {
		updated, err := queries.SetMCPServerRequiresUpstreamAuth(ctx, repo.SetMCPServerRequiresUpstreamAuthParams{
			RequiresUpstreamAuth: pgtype.Bool{Bool: required, Valid: true},
			ID:                   server.ID,
			ProjectID:            server.ProjectID,
			OrganizationID:       authCtx.ActiveOrganizationID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "set mcp server upstream auth requirement").LogError(ctx, logger)
		}
		if updated == 0 {
			return nil, oops.E(oops.CodeNotFound, nil, "mcp server not found").LogError(ctx, logger)
		}

		if err := s.audit.LogMcpServerUpstreamAuthRequirementUpdate(ctx, dbtx, audit.LogMcpServerUpstreamAuthRequirementUpdateEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        server.ProjectID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			McpServerURN:     urn.NewMcpServer(server.ID),
			McpServerName:    server.Name.String,
			McpServerSlug:    server.Slug.String,
			RequiredBefore:   conv.FromPGBool[bool](server.RequiresUpstreamAuth),
			RequiredAfter:    required,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "log mcp server upstream auth requirement").LogError(ctx, logger)
		}
	}

	view, err := s.serverAuthentication(ctx, queries, authCtx.ActiveOrganizationID, server.ID, server.ProjectID, server.Visibility, pgtype.Bool{Bool: required, Valid: true}, logger)
	if err != nil {
		return nil, err
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return view, nil
}

func (s *Service) require(ctx context.Context, scope authz.Scope) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, nil, oops.C(oops.CodeUnauthorized)
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	// Legacy API keys skip RBAC, so neither org:read nor org:admin would be
	// checked for them, and a project-bound one could not be confined to its
	// project. Only principal API keys, which carry grants, may manage auth
	// clients.
	if mode, byKey := contextvalues.APIKeyAuthorization(ctx); byKey && mode != contextvalues.APIKeyAuthorizationModePrincipal {
		return nil, nil, oops.E(oops.CodeForbidden, nil, "legacy API keys cannot manage the organization's auth clients").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: scope, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, nil, err
	}

	return authCtx, logger, nil
}

func (s *Service) serverAuthentication(ctx context.Context, queries *repo.Queries, organizationID string, serverID, projectID uuid.UUID, serverVisibility string, requires pgtype.Bool, logger *slog.Logger) (*types.McpServerAuthentication, error) {
	view, err := serverAuthenticationView(ctx, queries, organizationID, serverID, projectID, serverVisibility, requires)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read server authentication").LogError(ctx, logger)
	}
	return view, nil
}

// ReadServerAuthentication reports how one MCP server in the organization
// authenticates to its upstream, for surfaces other than this service. It
// performs no authorization: callers check access to the server first. A
// server outside the organization returns pgx.ErrNoRows.
func ReadServerAuthentication(ctx context.Context, db repo.DBTX, organizationID string, serverID uuid.UUID) (*types.McpServerAuthentication, error) {
	queries := repo.New(db)
	server, err := queries.GetOrganizationMCPServer(ctx, repo.GetOrganizationMCPServerParams{ID: serverID, OrganizationID: organizationID})
	if err != nil {
		return nil, fmt.Errorf("get mcp server: %w", err)
	}
	return serverAuthenticationView(ctx, queries, organizationID, server.ID, server.ProjectID, server.Visibility, server.RequiresUpstreamAuth)
}

func serverAuthenticationView(ctx context.Context, queries *repo.Queries, organizationID string, serverID, projectID uuid.UUID, serverVisibility string, requires pgtype.Bool) (*types.McpServerAuthentication, error) {
	clients, err := queries.ListServerAuthClients(ctx, repo.ListServerAuthClientsParams{
		McpServerID:    serverID,
		ProjectID:      projectID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("list server auth clients: %w", err)
	}

	views, err := clientViews(ctx, queries, organizationID, clients)
	if err != nil {
		return nil, err
	}

	return &types.McpServerAuthentication{
		McpServerID:          serverID.String(),
		ProjectID:            projectID.String(),
		Public:               serverVisibility == visibility.Public,
		RequiresUpstreamAuth: conv.FromPGBool[bool](requires),
		Status:               upstreamAuthStatus(requires, clients),
		Clients:              views,
	}, nil
}

// upstreamAuthStatus separates a server that explicitly needs no
// authentication from one that needs it and offers no usable client: having
// no clients is never read as needing none.
func upstreamAuthStatus(requires pgtype.Bool, clients []repo.McpAuthClient) string {
	switch {
	case !requires.Valid:
		return StatusUndecided
	case !requires.Bool:
		return StatusNoAuthRequired
	}
	for _, c := range clients {
		if c.Enabled {
			return StatusConfigured
		}
	}
	return StatusSetupIncomplete
}

func (s *Service) view(ctx context.Context, queries *repo.Queries, organizationID string, client repo.McpAuthClient, logger *slog.Logger) (*types.AuthClient, error) {
	views, err := clientViews(ctx, queries, organizationID, []repo.McpAuthClient{client})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build auth client view").LogError(ctx, logger)
	}
	return views[0], nil
}

func clientViews(ctx context.Context, queries *repo.Queries, organizationID string, clients []repo.McpAuthClient) ([]*types.AuthClient, error) {
	ids := make([]uuid.UUID, 0, len(clients))
	for _, c := range clients {
		ids = append(ids, c.ID)
	}

	headers, err := queries.ListAuthClientHeaders(ctx, repo.ListAuthClientHeadersParams{OrganizationID: organizationID, AuthClientIds: ids})
	if err != nil {
		return nil, fmt.Errorf("list auth client headers: %w", err)
	}
	servers, err := queries.ListAuthClientServers(ctx, repo.ListAuthClientServersParams{OrganizationID: organizationID, AuthClientIds: ids})
	if err != nil {
		return nil, fmt.Errorf("list auth client servers: %w", err)
	}

	headersByClient := make(map[uuid.UUID][]repo.ListAuthClientHeadersRow, len(clients))
	for _, h := range headers {
		headersByClient[h.AuthClientID] = append(headersByClient[h.AuthClientID], h)
	}
	serversByClient := make(map[uuid.UUID][]repo.ListAuthClientServersRow, len(clients))
	for _, srv := range servers {
		serversByClient[srv.AuthClientID] = append(serversByClient[srv.AuthClientID], srv)
	}

	views := make([]*types.AuthClient, 0, len(clients))
	for _, c := range clients {
		view := buildAuthClientView(c, headersByClient[c.ID], serversByClient[c.ID])
		if c.ClientType == "service_account" && !c.RemoteSessionClientID.Valid {
			view.GithubApp, err = githubAppView(ctx, queries, organizationID, c.ID)
			if err != nil {
				return nil, err
			}
		}
		views = append(views, view)
	}
	return views, nil
}

func parseOptionalUUID(value *string) (uuid.NullUUID, error) {
	if value == nil || *value == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}
	id, err := uuid.Parse(*value)
	if err != nil {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, fmt.Errorf("parse uuid: %w", err)
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}
