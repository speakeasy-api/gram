package usage

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"

	"github.com/jackc/pgx/v5/pgxpool"
	polargo "github.com/polarsource/polar-go"
	srv "github.com/speakeasy-api/gram/server/gen/http/usage/server"
	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
)

type Service struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	auth        *auth.Auth
	polarClient *PolarClient
	serverURL   *url.URL
	repo        *repo.Queries
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, polar *polargo.Polar, serverURL *url.URL) *Service {
	logger = logger.With(attr.SlogComponent("usage"))

	polarClient := NewPolarClient(polar, logger)

	return &Service{
		tracer:      otel.Tracer("github.com/speakeasy-api/gram/server/internal/usage"),
		logger:      logger,
		auth:        auth.New(logger, db, sessions),
		polarClient: polarClient,
		serverURL:   serverURL,
		repo:        repo.New(db),
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

func (s *Service) GetPeriodUsage(ctx context.Context, payload *gen.GetPeriodUsagePayload) (res *gen.PeriodUsage, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	polarUsage, err := s.polarClient.GetPeriodUsage(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, err
	}

	actualPublicServerCount, err := s.repo.GetPublicServerCount(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "Could not get public server count")
	}

	return &gen.PeriodUsage{
		ToolCalls:               polarUsage.ToolCalls,
		MaxToolCalls:            polarUsage.MaxToolCalls,
		Servers:                 polarUsage.Servers,
		MaxServers:              polarUsage.MaxServers,
		ActualPublicServerCount: int(actualPublicServerCount),
	}, nil
}

func (s *Service) GetUsageTiers(ctx context.Context) (res *gen.UsageTiers, err error) {
	product, err := s.polarClient.GetGramBusinessProduct(ctx)
	if err != nil {
		return nil, err
	}

	j, _ := json.Marshal(product)
	println(j)

	return &gen.UsageTiers{
		Free: &gen.TierLimits{
			BasePrice: 0,
			IncludedToolCalls: 1000,
			IncludedServers: 1,
			PricePerAdditionalToolCall: 0.0001,
			PricePerAdditionalServer: 0.0001,
		},
		Business: &gen.TierLimits{
			BasePrice: 0,
			IncludedToolCalls: 1000,
			IncludedServers: 1,
			PricePerAdditionalToolCall: 0.0001,
			PricePerAdditionalServer: 0.0001,
		},
	}, nil
}

func (s *Service) CreateCheckout(ctx context.Context, payload *gen.CreateCheckoutPayload) (res string, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return "", oops.C(oops.CodeUnauthorized)
	}

	return s.polarClient.CreateCheckout(ctx, authCtx.ActiveOrganizationID, s.serverURL.String())
}

func (s *Service) CreateCustomerSession(ctx context.Context, payload *gen.CreateCustomerSessionPayload) (res string, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return "", oops.C(oops.CodeUnauthorized)
	}

	return s.polarClient.CreateCustomerSession(ctx, authCtx.ActiveOrganizationID)
}
