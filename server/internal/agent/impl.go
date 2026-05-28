package agent

import (
	"context"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	srv "github.com/speakeasy-api/gram/server/gen/http/agent/server"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	db        *pgxpool.Pool
	repo      *repo.Queries
	auth      *auth.Auth
	serverURL string
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	serverURL string,
) *Service {
	logger = logger.With(attr.SlogComponent("agent"))
	return &Service{
		tracer:    tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/agent"),
		logger:    logger,
		db:        db,
		repo:      repo.New(db),
		auth:      auth.New(logger, db, sessions, authzEngine),
		serverURL: serverURL,
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

// GetPlugins resolves the principal set for the supplied email and returns
// every plugin assigned to any of those principals within the caller's org.
//
// MVP principal resolution: `email:<addr>` (lowercased + trimmed) plus the
// wildcard `*`. The followup ticket will add `user:<id>` (from Gram user
// lookup) and `role:<name>` (from RBAC role membership) so admins can assign
// plugins to existing users and roles in addition to raw emails.
func (s *Service) GetPlugins(ctx context.Context, payload *gen.GetPluginsPayload) (*gen.GetPluginsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	email := strings.TrimSpace(strings.ToLower(payload.Email))
	if email == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "email is required")
	}

	principals := []string{
		"email:" + email,
		"*",
	}

	rows, err := s.repo.GetAssignedPluginsWithServers(ctx, repo.GetAssignedPluginsWithServersParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrns:  principals,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving assigned plugins").Log(ctx, s.logger)
	}

	return mv.BuildAgentPluginsView(s.serverURL, rows), nil
}
