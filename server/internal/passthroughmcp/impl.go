package passthroughmcp

import (
	"context"
	"errors"
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

	srv "github.com/speakeasy-api/gram/server/gen/http/passthrough_mcp/server"
	gen "github.com/speakeasy-api/gram/server/gen/passthrough_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/passthroughmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	authz  *authz.Engine
	audit  *audit.Logger
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
) *Service {
	logger = logger.With(attr.SlogComponent("passthroughmcp"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/passthroughmcp"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
		audit:  auditLogger,
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

// isSpeakeasyStaffEmail reports whether email belongs to a Speakeasy-owned
// domain. Mirrors the domain half of access.Service.requirePlatformAdmin
// (server/internal/access/impl.go), without that function's DB admin-flag
// fallback: pass-through MCP servers are gated on domain only, not on the
// separate platform-admin flag.
func isSpeakeasyStaffEmail(email string) bool {
	return strings.HasSuffix(email, "@speakeasy.com") || strings.HasSuffix(email, "@speakeasyapi.dev")
}

func (s *Service) CreateServer(ctx context.Context, payload *gen.CreateServerPayload) (*types.PassthroughMcpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	email := ""
	if authCtx.Email != nil {
		email = *authCtx.Email
	}
	if !isSpeakeasyStaffEmail(email) {
		return nil, oops.E(oops.CodeForbidden, nil, "pass-through MCP servers can only be added by Speakeasy staff").LogWarn(ctx, logger)
	}

	// Generate the server ID up front so the slug can include its suffix and
	// the row can be inserted in a single statement (no insert-then-update).
	serverID, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate server id").LogError(ctx, logger)
	}

	slug, err := computeServerSlug(payload.URL, serverID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "compute server slug").LogError(ctx, logger)
	}

	name := pgtype.Text{String: "", Valid: false}
	if payload.Name != nil {
		if trimmed := strings.TrimSpace(*payload.Name); trimmed != "" {
			name = pgtype.Text{String: trimmed, Valid: true}
		}
	}

	description := pgtype.Text{String: "", Valid: false}
	if payload.Description != nil {
		if trimmed := strings.TrimSpace(*payload.Description); trimmed != "" {
			description = pgtype.Text{String: trimmed, Valid: true}
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	server, err := txRepo.CreateServer(ctx, repo.CreateServerParams{
		ID:          serverID,
		ProjectID:   *authCtx.ProjectID,
		Name:        name,
		Slug:        conv.ToPGText(slug),
		Url:         payload.URL,
		Description: description,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "pass-through mcp server slug already in use").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create pass-through mcp server").LogError(ctx, logger)
	}

	if err := s.audit.LogPassthroughMcpServerCreate(ctx, dbtx, audit.LogPassthroughMcpServerCreateEvent{
		OrganizationID:          authCtx.ActiveOrganizationID,
		ProjectID:               *authCtx.ProjectID,
		Actor:                   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:        authCtx.Email,
		ActorSlug:               nil,
		PassthroughMcpServerURN: urn.NewPassthroughMcpServer(server.ID),
		PassthroughMcpServerURL: server.Url,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log pass-through mcp server creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildPassthroughMcpServerView(server), nil
}

func (s *Service) ListServers(ctx context.Context, payload *gen.ListServersPayload) (*gen.ListPassthroughMcpServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	servers, err := repo.New(s.db).ListServersByProjectID(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list pass-through mcp servers").LogError(ctx, s.logger)
	}

	result := make([]*types.PassthroughMcpServer, 0, len(servers))
	for _, server := range servers {
		result = append(result, mv.BuildPassthroughMcpServerView(server))
	}

	return &gen.ListPassthroughMcpServersResult{PassthroughMcpServers: result}, nil
}

func (s *Service) GetServer(ctx context.Context, payload *gen.GetServerPayload) (*types.PassthroughMcpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	idProvided := payload.ID != nil && *payload.ID != ""
	slugProvided := payload.Slug != nil && *payload.Slug != ""
	if !idProvided && !slugProvided {
		return nil, oops.E(oops.CodeBadRequest, nil, "id or slug is required").LogError(ctx, s.logger)
	}
	if idProvided && slugProvided {
		return nil, oops.E(oops.CodeBadRequest, nil, "id and slug are mutually exclusive").LogError(ctx, s.logger)
	}

	dbRepo := repo.New(s.db)

	var server repo.PassthroughMcpServer
	var err error
	if idProvided {
		serverID, parseErr := uuid.Parse(*payload.ID)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid server id").LogError(ctx, s.logger)
		}
		server, err = dbRepo.GetServerByID(ctx, repo.GetServerByIDParams{
			ID:        serverID,
			ProjectID: *authCtx.ProjectID,
		})
	} else {
		server, err = dbRepo.GetServerBySlug(ctx, repo.GetServerBySlugParams{
			Slug:      conv.ToPGText(*payload.Slug),
			ProjectID: *authCtx.ProjectID,
		})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "pass-through mcp server not found").LogError(ctx, s.logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get pass-through mcp server").LogError(ctx, s.logger)
	}

	return mv.BuildPassthroughMcpServerView(server), nil
}

func (s *Service) DeleteServer(ctx context.Context, payload *gen.DeleteServerPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
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

	txRepo := repo.New(dbtx)

	deleted, err := txRepo.DeleteServer(ctx, repo.DeleteServerParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}

		return oops.E(oops.CodeUnexpected, err, "delete pass-through mcp server").LogError(ctx, logger)
	}

	if err := s.audit.LogPassthroughMcpServerDelete(ctx, dbtx, audit.LogPassthroughMcpServerDeleteEvent{
		OrganizationID:          authCtx.ActiveOrganizationID,
		ProjectID:               *authCtx.ProjectID,
		Actor:                   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:        authCtx.Email,
		ActorSlug:               nil,
		PassthroughMcpServerURN: urn.NewPassthroughMcpServer(deleted.ID),
		PassthroughMcpServerURL: deleted.Url,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log pass-through mcp server deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}
