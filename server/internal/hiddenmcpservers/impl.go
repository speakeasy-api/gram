package hiddenmcpservers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/hidden_mcp_servers"
	gen "github.com/speakeasy-api/gram/server/gen/http/hidden_mcp_servers/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/hiddenmcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
}

var (
	_ srv.Service = (*Service)(nil)
	_ srv.Auther  = (*Service)(nil)
)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessionManager *sessions.Manager, authzEngine *authz.Engine) *Service {
	logger = logger.With(attr.SlogComponent("hiddenmcpservers"))
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/hiddenmcpservers"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessionManager, authzEngine),
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := srv.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	gen.Mount(
		mux,
		gen.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// caller is the signed-in user. The list is the user's own, so no scope is
// needed, but a support session never changes it.
func (s *Service) caller(ctx context.Context, mutation bool) (*contextvalues.AuthContext, *slog.Logger, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.UserID == "" || authCtx.ActiveOrganizationID == "" {
		return nil, s.logger, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID), attr.SlogUserID(authCtx.UserID))
	if mutation && contextvalues.IsSupportSession(ctx) {
		return nil, logger, oops.E(oops.CodeForbidden, nil, "hidden servers cannot be changed from a support session").LogWarn(ctx, logger)
	}
	return authCtx, logger, nil
}

func (s *Service) List(ctx context.Context, _ *srv.ListPayload) (*srv.ListHiddenMcpServersResult, error) {
	authCtx, logger, err := s.caller(ctx, false)
	if err != nil {
		return nil, err
	}
	rows, err := repo.New(s.db).ListHiddenMCPServers(ctx, repo.ListHiddenMCPServersParams{OrganizationID: authCtx.ActiveOrganizationID, UserID: authCtx.UserID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list hidden servers").LogError(ctx, logger)
	}
	servers := make([]*srv.HiddenMcpServer, 0, len(rows))
	for _, row := range rows {
		servers = append(servers, &srv.HiddenMcpServer{
			McpServerID: row.McpServerID.String(),
			ProjectID:   row.ProjectID.String(),
			ProjectSlug: row.ProjectSlug,
			Slug:        conv.FromPGText[string](row.Slug),
			Name:        conv.FromPGText[string](row.Name),
			HiddenAt:    row.CreatedAt.Time.UTC().Format(time.RFC3339),
		})
	}
	return &srv.ListHiddenMcpServersResult{Servers: servers}, nil
}

func (s *Service) Hide(ctx context.Context, payload *srv.HidePayload) error {
	authCtx, logger, serverID, err := s.mutation(ctx, payload.McpServerID)
	if err != nil {
		return err
	}
	if err := Hide(ctx, s.db, authCtx.ActiveOrganizationID, authCtx.UserID, serverID); err != nil {
		if errors.Is(err, ErrServerNotFound) {
			return oops.E(oops.CodeNotFound, err, "mcp server not found")
		}
		return oops.E(oops.CodeUnexpected, err, "hide server").LogError(ctx, logger)
	}
	return nil
}

func (s *Service) Unhide(ctx context.Context, payload *srv.UnhidePayload) error {
	authCtx, logger, serverID, err := s.mutation(ctx, payload.McpServerID)
	if err != nil {
		return err
	}
	if err := Unhide(ctx, s.db, authCtx.ActiveOrganizationID, authCtx.UserID, serverID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "unhide server").LogError(ctx, logger)
	}
	return nil
}

func (s *Service) mutation(ctx context.Context, rawID string) (*contextvalues.AuthContext, *slog.Logger, uuid.UUID, error) {
	authCtx, logger, err := s.caller(ctx, true)
	if err != nil {
		return nil, logger, uuid.Nil, err
	}
	serverID, err := uuid.Parse(rawID)
	if err != nil {
		return nil, logger, uuid.Nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id")
	}
	return authCtx, logger, serverID, nil
}
