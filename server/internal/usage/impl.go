package usage

import (
	"context"
	"log/slog"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	polargo "github.com/polarsource/polar-go"
	polarComponents "github.com/polarsource/polar-go/models/components"
	srv "github.com/speakeasy-api/gram/server/gen/http/usage/server"
	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/polar"
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
	polarClient *polar.Client
	serverURL   *url.URL
	repo        *repo.Queries
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, polarClientRaw *polargo.Polar, serverURL *url.URL, redisClient *redis.Client) *Service {
	logger = logger.With(attr.SlogComponent("usage"))

	polarClient := polar.NewClient(polarClientRaw, logger, redisClient)

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

	// The actual number of public servers right this moment, which may not be updated in Polar yet.
	actualPublicServerCount, err := s.repo.GetPublicServerCount(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "Could not get public server count")
	}

	// We don't populate the maximums using GetUsageTiers because we want to reflect the actual granted credits, not the current product limits which may have changed.
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

	var toolCallsIncluded, mcpServersIncluded int64
	var toolCallPrice, mcpServerPrice float64

	for _, benefit := range product.Benefits {
		if benefit.Type == polarComponents.BenefitUnionTypeBenefitMeterCredit && benefit.BenefitMeterCredit != nil {
			if benefit.BenefitMeterCredit.Properties.MeterID == polar.ToolCallsMeterID {
				toolCallsIncluded = benefit.BenefitMeterCredit.Properties.Units
			}
			if benefit.BenefitMeterCredit.Properties.MeterID == polar.ServersMeterID {
				mcpServersIncluded = benefit.BenefitMeterCredit.Properties.Units
			}
		}
	}

	for _, price := range product.Prices {
		if price.Type == polarComponents.PricesTypeProductPrice && price.ProductPrice != nil && price.ProductPrice.ProductPriceMeteredUnit != nil {
			if price.ProductPrice.ProductPriceMeteredUnit.MeterID == polar.ToolCallsMeterID {
				meterPrice := *price.ProductPrice.ProductPriceMeteredUnit
				toolCallPrice, err = strconv.ParseFloat(meterPrice.UnitAmount, 64)
				if err != nil {
					return nil, err
				}
				toolCallPrice = toolCallPrice / 100 // Result from Polar is in cents
			}
			if price.ProductPrice.ProductPriceMeteredUnit.MeterID == polar.ServersMeterID {
				meterPrice := *price.ProductPrice.ProductPriceMeteredUnit
				mcpServerPrice, err = strconv.ParseFloat(meterPrice.UnitAmount, 64)
				if err != nil {
					return nil, err
				}
				mcpServerPrice = mcpServerPrice / 100 // Result from Polar is in cents
			}
		}
	}

	return &gen.UsageTiers{
		Free: &gen.TierLimits{
			BasePrice: 0,
			IncludedToolCalls: polar.FreeTierToolCalls,
			IncludedServers: polar.FreeTierServers,
			PricePerAdditionalToolCall: 0,
			PricePerAdditionalServer: 0,
		},
		Business: &gen.TierLimits{
			BasePrice: 0,
			IncludedToolCalls: int(toolCallsIncluded),
			IncludedServers: int(mcpServersIncluded),
			PricePerAdditionalToolCall: toolCallPrice,
			PricePerAdditionalServer: mcpServerPrice,
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
