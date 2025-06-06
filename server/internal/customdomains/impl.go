package customdomains

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/gen/domains"
	srv "github.com/speakeasy-api/gram/gen/http/domains/server"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/oops"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	repo   *repo.Queries
	auth   *auth.Auth
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/internal/customdomains"),
		logger: logger,
		repo:   repo.New(db),
		auth:   auth.New(logger, db, sessions),
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

func (s *Service) GetDomain(ctx context.Context, payload *gen.GetDomainPayload) (res *gen.CustomDomain, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID != "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	domain, err := s.repo.GetCustomDomainsByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "no custom domain found for organization").Log(ctx, s.logger)
	}

	return &gen.CustomDomain{
		ID:             domain.ID.String(),
		OrganizationID: domain.OrganizationID,
		Domain:         domain.Domain,
		Verified:       domain.Verified,
		Activated:      domain.Activated,
		CreatedAt:      domain.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      domain.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) CreateDomain(ctx context.Context, payload *gen.CreateDomainPayload) (res *gen.CustomDomain, err error) {
	// TODO: To start domain registration will be kicked off on-demand
	return nil, nil
}

func (s *Service) DeleteDomain(context.Context, *gen.DeleteDomainPayload) (err error) {
	// TODO: To start domain de-registration will be kicked off on-demand
	return nil
}
