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
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/marketplace"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

// GetPlugins returns every plugin assigned to the resolved principal set for
// the supplied email within the caller's org. user: and role: resolution
// lands in the follow-up ticket alongside email→user_id lookup and RBAC
// role-membership reads.
func (s *Service) GetPlugins(ctx context.Context, payload *gen.GetPluginsPayload) (*gen.GetPluginsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	email := conv.NormalizeEmail(payload.Email)
	emailPrincipal, err := urn.ParsePrincipal(string(urn.PrincipalTypeEmail) + ":" + email)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid email")
	}

	principals := []string{
		emailPrincipal.String(),
		urn.PrincipalWildcard,
	}

	rows, err := s.repo.GetAgentPluginSet(ctx, repo.GetAgentPluginSetParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		PrincipalUrns:  principals,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error resolving agent plugin set").Log(ctx, s.logger)
	}

	base := strings.TrimRight(s.serverURL, "/")
	marketplaceURL := func(token string) string {
		return base + marketplace.RoutePrefix + token + ".git"
	}

	return mv.BuildAgentPluginsView(rows, marketplaceURL), nil
}
