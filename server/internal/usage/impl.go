package usage

import (
	"context"
	"log/slog"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
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
	tracer    trace.Tracer
	logger    *slog.Logger
	auth      *auth.Auth
	serverURL *url.URL
	repo      *repo.Queries
	polar     *Client
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, polar *Client, serverURL *url.URL) *Service {
	logger = logger.With(attr.SlogComponent("usage"))

	return &Service{
		tracer:    otel.Tracer("github.com/speakeasy-api/gram/server/internal/usage"),
		logger:    logger,
		auth:      auth.New(logger, db, sessions),
		serverURL: serverURL,
		repo:      repo.New(db),
		polar:     polar,
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

	polarUsage, err := s.polar.GetPeriodUsage(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get period usage").Log(ctx, s.logger)
	}

	// The actual number of public servers right this moment, which may not be updated in Polar yet.
	actualPublicServerCount, err := s.repo.GetPublicServerCount(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not get public server count").Log(ctx, s.logger)
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
	freeTierProduct, err := s.polar.GetGramFreeTierProduct(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get gram free tier product").Log(ctx, s.logger)
	}

	proProduct, err := s.polar.GetGramProProduct(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get gram business product").Log(ctx, s.logger)
	}

	freeTierLimits := polar.ExtractTierLimits(freeTierProduct)
	proTierLimits := polar.ExtractTierLimits(proProduct)

	var toolCallPrice, mcpServerPrice float64

	for _, price := range proProduct.Prices {
		if price.Type != polarComponents.PricesTypeProductPrice {
			continue
		}
		if price.ProductPrice == nil || price.ProductPrice.ProductPriceMeteredUnit == nil {
			continue
		}

		if price.ProductPrice.ProductPriceMeteredUnit.MeterID == polar.MeterIDToolCalls {
			meterPrice := *price.ProductPrice.ProductPriceMeteredUnit
			toolCallPrice, err = strconv.ParseFloat(meterPrice.UnitAmount, 64)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to parse tool call price").Log(ctx, s.logger)
			}
			toolCallPrice /= 100 // Result from Polar is in cents
		}

		if price.ProductPrice.ProductPriceMeteredUnit.MeterID == polar.MeterIDServers {
			meterPrice := *price.ProductPrice.ProductPriceMeteredUnit
			mcpServerPrice, err = strconv.ParseFloat(meterPrice.UnitAmount, 64)
			if err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "failed to parse mcp server price").Log(ctx, s.logger)
			}
			mcpServerPrice /= 100 // Result from Polar is in cents
		}
	}

	return &gen.UsageTiers{
		Free: &gen.TierLimits{
			BasePrice:                  0,
			IncludedToolCalls:          freeTierLimits.ToolCalls,
			IncludedServers:            freeTierLimits.Servers,
			PricePerAdditionalToolCall: 0,
			PricePerAdditionalServer:   0,
		},
		Business: &gen.TierLimits{
			BasePrice:                  0,
			IncludedToolCalls:          proTierLimits.ToolCalls,
			IncludedServers:            proTierLimits.Servers,
			PricePerAdditionalToolCall: toolCallPrice,
			PricePerAdditionalServer:   mcpServerPrice,
		},
	}, nil
}

func (s *Service) CreateCheckout(ctx context.Context, payload *gen.CreateCheckoutPayload) (res string, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return "", oops.C(oops.CodeUnauthorized)
	}

	checkoutURL, err := s.polar.CreateCheckout(ctx, authCtx.ActiveOrganizationID, s.serverURL.String())
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to create checkout").Log(ctx, s.logger)
	}
	return checkoutURL, nil
}

func (s *Service) CreateCustomerSession(ctx context.Context, payload *gen.CreateCustomerSessionPayload) (res string, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return "", oops.C(oops.CodeUnauthorized)
	}

	sessionURL, err := s.polar.CreateCustomerSession(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to create customer session").Log(ctx, s.logger)
	}
	return sessionURL, nil
}
