package tunneledmcp

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

	srv "github.com/speakeasy-api/gram/server/gen/http/tunneled_mcp/server"
	gen "github.com/speakeasy-api/gram/server/gen/tunneled_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/tunnel/route"
)

type Service struct {
	tracer        trace.Tracer
	logger        *slog.Logger
	db            *pgxpool.Pool
	auth          *auth.Auth
	authz         *authz.Engine
	audit         *audit.Logger
	tunnelManager *tunnelManager
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
	runtime route.RuntimeStore,
) *Service {
	logger = logger.With(attr.SlogComponent("tunneledmcp"))

	return &Service{
		tracer:        tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/tunneledmcp"),
		logger:        logger,
		db:            db,
		auth:          auth.New(logger, db, sessions, authzEngine),
		authz:         authzEngine,
		audit:         auditLogger,
		tunnelManager: newTunnelManager(runtime),
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

func (s *Service) CreateServer(ctx context.Context, payload *gen.CreateServerPayload) (*gen.CreateTunneledMcpServerResult, error) {
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
		return nil, oops.E(oops.CodeBadRequest, nil, "name must be non-empty").LogWarn(ctx, logger)
	}

	serverID, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate server id").LogError(ctx, logger)
	}

	issuedKey, err := s.tunnelManager.issueKey()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate tunnel key").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)
	if err := txRepo.LockOrganizationTunneledMcpLimit(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock tunneled mcp server limit").LogError(ctx, logger)
	}
	configuredLimit, err := txRepo.GetTunneledMcpServerLimitByOrganizationID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get tunneled mcp server limit").LogError(ctx, logger)
	}
	limit := effectiveTunneledMcpServerLimit(authCtx.AccountType, configuredLimit)
	activeCount, err := txRepo.CountActiveServersByOrganizationID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count tunneled mcp servers").LogError(ctx, logger)
	}
	if activeCount >= limit {
		return nil, oops.E(oops.CodeForbidden, fmt.Errorf("tunneled mcp server limit reached: %d", limit), "tunneled mcp server limit reached").LogWarn(ctx, logger)
	}

	server, err := txRepo.CreateServer(ctx, repo.CreateServerParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
		Name:      name,
		KeyHash:   issuedKey.Hash,
		KeyPrefix: issuedKey.Prefix,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "tunneled mcp server already exists").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create tunneled mcp server").LogError(ctx, logger)
	}

	if err := s.audit.LogTunneledMcpServerCreate(ctx, dbtx, audit.LogTunneledMcpServerCreateEvent{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:      authCtx.Email,
		ActorSlug:             nil,
		TunneledMcpServerURN:  urn.NewTunneledMcpServer(server.ID),
		TunneledMcpServerName: server.Name,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log tunneled mcp server creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return &gen.CreateTunneledMcpServerResult{
		Server:    s.tunnelManager.serverViewWithoutRuntime(server),
		TunnelKey: issuedKey.Plaintext,
	}, nil
}

func (s *Service) ListServers(ctx context.Context, payload *gen.ListServersPayload) (*gen.ListTunneledMcpServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	servers, err := repo.New(s.db).ListServersByProjectID(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list tunneled mcp servers").LogError(ctx, s.logger)
	}

	return &gen.ListTunneledMcpServersResult{TunneledMcpServers: s.tunnelManager.serverListView(ctx, servers)}, nil
}

func (s *Service) GetServer(ctx context.Context, payload *gen.GetServerPayload) (*types.TunneledMcpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogWarn(ctx, s.logger)
	}

	server, err := repo.New(s.db).GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "tunneled mcp server not found").LogWarn(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get tunneled mcp server").LogError(ctx, s.logger)
	}

	return s.tunnelManager.serverView(ctx, server), nil
}

func (s *Service) GetServerConnections(ctx context.Context, payload *gen.GetServerConnectionsPayload) (*types.TunneledMcpServerConnections, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogWarn(ctx, s.logger)
	}

	server, err := repo.New(s.db).GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "tunneled mcp server not found").LogWarn(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get tunneled mcp server").LogError(ctx, s.logger)
	}

	return s.tunnelManager.serverConnectionsView(ctx, server.ID), nil
}

func (s *Service) UpdateServer(ctx context.Context, payload *gen.UpdateServerPayload) (*types.TunneledMcpServer, error) {
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
		return nil, oops.E(oops.CodeBadRequest, nil, "name must be non-empty").LogWarn(ctx, logger)
	}

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogWarn(ctx, logger)
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
			return nil, oops.E(oops.CodeNotFound, err, "tunneled mcp server not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get tunneled mcp server").LogError(ctx, logger)
	}

	beforeView := s.tunnelManager.serverView(ctx, existing)

	updated, err := txRepo.UpdateServer(ctx, repo.UpdateServerParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
		Name:      name,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "tunneled mcp server name already in use").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update tunneled mcp server").LogError(ctx, logger)
	}

	afterView := s.tunnelManager.serverView(ctx, updated)
	if err := s.audit.LogTunneledMcpServerUpdate(ctx, dbtx, audit.LogTunneledMcpServerUpdateEvent{
		OrganizationID:                  authCtx.ActiveOrganizationID,
		ProjectID:                       *authCtx.ProjectID,
		Actor:                           urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                authCtx.Email,
		ActorSlug:                       nil,
		TunneledMcpServerURN:            urn.NewTunneledMcpServer(updated.ID),
		TunneledMcpServerName:           updated.Name,
		TunneledMcpServerSnapshotBefore: beforeView,
		TunneledMcpServerSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log tunneled mcp server update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) RotateServerKey(ctx context.Context, payload *gen.RotateServerKeyPayload) (*gen.RotateTunneledMcpServerKeyResult, error) {
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogWarn(ctx, logger)
	}

	issuedKey, err := s.tunnelManager.issueKey()
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
			return nil, oops.E(oops.CodeNotFound, err, "tunneled mcp server not found").LogWarn(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get tunneled mcp server").LogError(ctx, logger)
	}

	beforeView := s.tunnelManager.serverView(ctx, existing)
	rotated, err := txRepo.RotateServerKey(ctx, repo.RotateServerKeyParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
		KeyHash:   issuedKey.Hash,
		KeyPrefix: issuedKey.Prefix,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "rotate tunneled mcp server key").LogError(ctx, logger)
	}

	afterView := s.tunnelManager.serverViewWithoutRuntime(rotated)
	if err := s.audit.LogTunneledMcpServerRotate(ctx, dbtx, audit.LogTunneledMcpServerRotateEvent{
		OrganizationID:                  authCtx.ActiveOrganizationID,
		ProjectID:                       *authCtx.ProjectID,
		Actor:                           urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                authCtx.Email,
		ActorSlug:                       nil,
		TunneledMcpServerURN:            urn.NewTunneledMcpServer(rotated.ID),
		TunneledMcpServerName:           rotated.Name,
		TunneledMcpServerSnapshotBefore: beforeView,
		TunneledMcpServerSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log tunneled mcp server key rotation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	s.tunnelManager.deleteRuntimeState(ctx, logger, serverID)

	return &gen.RotateTunneledMcpServerKeyResult{
		Server:    afterView,
		TunnelKey: issuedKey.Plaintext,
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
		return oops.E(oops.CodeBadRequest, err, "invalid server id").LogWarn(ctx, logger)
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
		return oops.E(oops.CodeUnexpected, err, "delete tunneled mcp server").LogError(ctx, logger)
	}

	if err := s.audit.LogTunneledMcpServerDelete(ctx, dbtx, audit.LogTunneledMcpServerDeleteEvent{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:      authCtx.Email,
		ActorSlug:             nil,
		TunneledMcpServerURN:  urn.NewTunneledMcpServer(deleted.ID),
		TunneledMcpServerName: deleted.Name,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log tunneled mcp server deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	s.tunnelManager.deleteRuntimeState(ctx, logger, serverID)

	return nil
}

func effectiveTunneledMcpServerLimit(accountType string, configured pgtype.Int4) int64 {
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
