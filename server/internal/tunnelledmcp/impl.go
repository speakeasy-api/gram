package tunnelledmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/tunnelled_mcp/server"
	gen "github.com/speakeasy-api/gram/server/gen/tunnelled_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/tunnelledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/tunnel/wire"
)

const routeCacheKeyPrefix = "tunnel_routes:"

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	authz  *authz.Engine
	audit  *audit.Logger
	cache  cache.Cache
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
	cacheAdapter cache.Cache,
) *Service {
	logger = logger.With(attr.SlogComponent("tunnelledmcp"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/tunnelledmcp"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
		audit:  auditLogger,
		cache:  cacheAdapter,
	}
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

func (s *Service) CreateServer(ctx context.Context, payload *gen.CreateServerPayload) (*gen.CreateTunnelledMcpServerResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name must be non-empty").LogError(ctx, logger)
	}

	serverID, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate server id").LogError(ctx, logger)
	}

	tunnelKey, keyHash, err := wire.NewKey()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate tunnel key").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)
	if err := txRepo.LockOrganizationTunnelledMcpLimit(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock tunnelled mcp server limit").LogError(ctx, logger)
	}
	configuredLimit, err := txRepo.GetTunnelledMcpServerLimitByOrganizationID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get tunnelled mcp server limit").LogError(ctx, logger)
	}
	limit := effectiveTunnelledMcpServerLimit(authCtx.AccountType, configuredLimit)
	activeCount, err := txRepo.CountActiveServersByOrganizationID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count tunnelled mcp servers").LogError(ctx, logger)
	}
	if activeCount >= limit {
		return nil, oops.E(oops.CodeForbidden, fmt.Errorf("tunnelled mcp server limit reached: %d", limit), "tunnelled mcp server limit reached").LogError(ctx, logger)
	}

	server, err := txRepo.CreateServer(ctx, repo.CreateServerParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
		Name:      name,
		KeyHash:   keyHash,
		KeyPrefix: tunnelKey[:len(wire.KeyPrefix)+5],
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "tunnelled mcp server already exists").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create tunnelled mcp server").LogError(ctx, logger)
	}

	if err := s.audit.LogTunnelledMcpServerCreate(ctx, dbtx, audit.LogTunnelledMcpServerCreateEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		TunnelledMcpServerURN:  urn.NewTunnelledMcpServer(server.ID),
		TunnelledMcpServerName: server.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log tunnelled mcp server creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return &gen.CreateTunnelledMcpServerResult{
		Server:    mv.BuildTunnelledMcpServerView(server, nil),
		TunnelKey: tunnelKey,
	}, nil
}

func (s *Service) ListServers(ctx context.Context, payload *gen.ListServersPayload) (*gen.ListTunnelledMcpServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	servers, err := repo.New(s.db).ListServersByProjectID(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list tunnelled mcp servers").LogError(ctx, s.logger)
	}

	connectionsByServerID := make(map[string][]mv.TunnelledMcpConnectionCache, len(servers))
	for _, server := range servers {
		connectionsByServerID[server.ID.String()] = s.connectionsForServer(ctx, server.ID)
	}

	return &gen.ListTunnelledMcpServersResult{TunnelledMcpServers: mv.BuildTunnelledMcpServerListView(servers, connectionsByServerID)}, nil
}

func (s *Service) GetServer(ctx context.Context, payload *gen.GetServerPayload) (*types.TunnelledMcpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogError(ctx, s.logger)
	}

	server, err := repo.New(s.db).GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "tunnelled mcp server not found").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get tunnelled mcp server").LogError(ctx, s.logger)
	}

	return mv.BuildTunnelledMcpServerView(server, s.connectionsForServer(ctx, server.ID)), nil
}

func (s *Service) UpdateServer(ctx context.Context, payload *gen.UpdateServerPayload) (*types.TunnelledMcpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name must be non-empty").LogError(ctx, logger)
	}

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)
	existing, err := txRepo.GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "tunnelled mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get tunnelled mcp server").LogError(ctx, logger)
	}

	beforeView := mv.BuildTunnelledMcpServerView(existing, s.connectionsForServer(ctx, existing.ID))

	updated, err := txRepo.UpdateServer(ctx, repo.UpdateServerParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
		Name:      name,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "tunnelled mcp server name already in use").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update tunnelled mcp server").LogError(ctx, logger)
	}

	afterView := mv.BuildTunnelledMcpServerView(updated, s.connectionsForServer(ctx, updated.ID))
	if err := s.audit.LogTunnelledMcpServerUpdate(ctx, dbtx, audit.LogTunnelledMcpServerUpdateEvent{
		OrganizationID:                   authCtx.ActiveOrganizationID,
		ProjectID:                        *authCtx.ProjectID,
		Actor:                            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                 authCtx.Email,
		ActorSlug:                        nil,
		TunnelledMcpServerURN:            urn.NewTunnelledMcpServer(updated.ID),
		TunnelledMcpServerName:           updated.Name,
		TunnelledMcpServerSnapshotBefore: beforeView,
		TunnelledMcpServerSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log tunnelled mcp server update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) RotateServerKey(ctx context.Context, payload *gen.RotateServerKeyPayload) (*gen.RotateTunnelledMcpServerKeyResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))
	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogError(ctx, logger)
	}

	tunnelKey, keyHash, err := wire.NewKey()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate tunnel key").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)
	existing, err := txRepo.GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "tunnelled mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get tunnelled mcp server").LogError(ctx, logger)
	}

	beforeView := mv.BuildTunnelledMcpServerView(existing, s.connectionsForServer(ctx, existing.ID))
	rotated, err := txRepo.RotateServerKey(ctx, repo.RotateServerKeyParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
		KeyHash:   keyHash,
		KeyPrefix: tunnelKey[:len(wire.KeyPrefix)+5],
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "rotate tunnelled mcp server key").LogError(ctx, logger)
	}

	afterView := mv.BuildTunnelledMcpServerView(rotated, nil)
	if err := s.audit.LogTunnelledMcpServerRotate(ctx, dbtx, audit.LogTunnelledMcpServerRotateEvent{
		OrganizationID:                   authCtx.ActiveOrganizationID,
		ProjectID:                        *authCtx.ProjectID,
		Actor:                            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                 authCtx.Email,
		ActorSlug:                        nil,
		TunnelledMcpServerURN:            urn.NewTunnelledMcpServer(rotated.ID),
		TunnelledMcpServerName:           rotated.Name,
		TunnelledMcpServerSnapshotBefore: beforeView,
		TunnelledMcpServerSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log tunnelled mcp server key rotation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	s.deleteRouteState(ctx, logger, serverID)

	return &gen.RotateTunnelledMcpServerKeyResult{
		Server:    afterView,
		TunnelKey: tunnelKey,
	}, nil
}

func (s *Service) DeleteServer(ctx context.Context, payload *gen.DeleteServerPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))
	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid server id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	deleted, err := repo.New(dbtx).DeleteServer(ctx, repo.DeleteServerParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return oops.E(oops.CodeUnexpected, err, "delete tunnelled mcp server").LogError(ctx, logger)
	}

	if err := s.audit.LogTunnelledMcpServerDelete(ctx, dbtx, audit.LogTunnelledMcpServerDeleteEvent{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:       authCtx.Email,
		ActorSlug:              nil,
		TunnelledMcpServerURN:  urn.NewTunnelledMcpServer(deleted.ID),
		TunnelledMcpServerName: deleted.Name,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log tunnelled mcp server deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	s.deleteRouteState(ctx, logger, serverID)

	return nil
}

type connectionCacheValue struct {
	Connections []mv.TunnelledMcpConnectionCache `json:"connections"`
}

func (s *Service) connectionsForServer(ctx context.Context, serverID uuid.UUID) []mv.TunnelledMcpConnectionCache {
	if s.cache == nil {
		return nil
	}

	value := connectionCacheValue{Connections: []mv.TunnelledMcpConnectionCache{}}
	if err := s.cache.Get(ctx, connectionCacheKey(serverID), &value); err != nil {
		return nil
	}

	return value.Connections
}

func connectionCacheKey(serverID uuid.UUID) string {
	return "tunnel_connections:" + serverID.String()
}

func routeCacheKey(serverID uuid.UUID) string {
	return routeCacheKeyPrefix + serverID.String()
}

func (s *Service) deleteRouteState(ctx context.Context, logger *slog.Logger, serverID uuid.UUID) {
	if s.cache == nil {
		return
	}
	if err := s.cache.Delete(ctx, connectionCacheKey(serverID)); err != nil {
		logger.WarnContext(ctx, "delete tunnelled mcp connection cache", attr.SlogError(err))
	}
	if err := s.cache.Delete(ctx, routeCacheKey(serverID)); err != nil {
		logger.WarnContext(ctx, "delete tunnelled mcp route cache", attr.SlogError(err))
	}
}

func effectiveTunnelledMcpServerLimit(accountType string, configured pgtype.Int4) int64 {
	if configured.Valid {
		return int64(configured.Int32)
	}

	switch billing.Tier(strings.ToLower(strings.TrimSpace(accountType))) {
	case billing.TierPro:
		return 10
	case billing.TierEnterprise:
		return 25
	default:
		return 0
	}
}
