package usage

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"
	srv "github.com/speakeasy-api/gram/server/gen/http/usage/server"
	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
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
	serverURL   *url.URL
	repo        *repo.Queries
	billingRepo billing.Repository
	orgRepo     *orgRepo.Queries
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, billingRepo billing.Repository, serverURL *url.URL) *Service {
	logger = logger.With(attr.SlogComponent("usage"))

	return &Service{
		tracer:      otel.Tracer("github.com/speakeasy-api/gram/server/internal/usage"),
		logger:      logger,
		auth:        auth.New(logger, db, sessions),
		serverURL:   serverURL,
		repo:        repo.New(db),
		billingRepo: billingRepo,
		orgRepo:     orgRepo.New(db),
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
	o11y.AttachHandler(mux, "POST", "/rpc/polar.webhook", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.HandlePolarWebhook).ServeHTTP(w, r)
	})
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) HandlePolarWebhook(w http.ResponseWriter, r *http.Request) error {
	acceptedEvents := []string{
		"subscription.created",
		"subscription.updated",
		"subscription.canceled",
		"subscription.uncanceled",
		"subscription.revoked",
	}
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to read request body").Log(ctx, s.logger)
	}
	defer o11y.LogDefer(ctx, s.logger, func() error {
		return r.Body.Close()
	})

	webhookPayload, err := s.billingRepo.ValidateAndParseWebhookEvent(ctx, body, r.Header)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to validate and parse webhook event").Log(ctx, s.logger)
	}

	if !slices.Contains(acceptedEvents, webhookPayload.Type) {
		s.logger.InfoContext(ctx, "skipping unsupported webhook event", attr.SlogEvent(webhookPayload.Type))
		return nil
	}

	if webhookPayload.Data.Customer == nil || webhookPayload.Data.Customer.ExternalID == "" {
		return oops.E(oops.CodeUnexpected, errors.New("missing customer external id in webhook payload"), "missing customer external id in webhook payload").Log(ctx, s.logger)
	}

	// we must invalidate the customer tier cache since customer tier may have changed witha subscription update
	if err := s.billingRepo.InvalidateBillingCustomerCaches(ctx, webhookPayload.Data.Customer.ExternalID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to invalidate customer tier cache").Log(ctx, s.logger)
	}

	// we force a refresh of the state of the organization since customer tier may have changed witha subscription update
	if _, err = mv.DescribeOrganization(ctx, s.logger, s.orgRepo, s.billingRepo, webhookPayload.Data.Customer.ExternalID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to update organization metadata").Log(ctx, s.logger)
	}

	// we force a refresh of the period usage since usage may have changed with a subscription update
	if _, err = s.billingRepo.GetPeriodUsage(ctx, webhookPayload.Data.Customer.ExternalID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get period usage").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) GetPeriodUsage(ctx context.Context, payload *gen.GetPeriodUsagePayload) (res *gen.PeriodUsage, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	periodUsage, err := s.billingRepo.GetPeriodUsage(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get period usage").Log(ctx, s.logger)
	}

	// The actual number of enabled servers right this moment, which may not be updated in Polar yet.
	actualEnabledServerCount, err := s.repo.GetEnabledServerCount(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not get public server count").Log(ctx, s.logger)
	}

	// We don't populate the maximums using GetUsageTiers because we want to reflect the actual granted credits, not the current product limits which may have changed.
	return &gen.PeriodUsage{
		ToolCalls:                periodUsage.ToolCalls,
		MaxToolCalls:             periodUsage.MaxToolCalls,
		Servers:                  periodUsage.Servers,
		MaxServers:               periodUsage.MaxServers,
		ActualEnabledServerCount: int(actualEnabledServerCount),
	}, nil
}

func (s *Service) GetUsageTiers(ctx context.Context) (*gen.UsageTiers, error) {
	tiers, err := s.billingRepo.GetUsageTiers(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to get usage tiers").Log(ctx, s.logger)
	}

	return tiers, nil
}

func (s *Service) CreateCheckout(ctx context.Context, payload *gen.CreateCheckoutPayload) (res string, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return "", oops.C(oops.CodeUnauthorized)
	}

	checkoutURL, err := s.billingRepo.CreateCheckout(ctx, authCtx.ActiveOrganizationID, s.serverURL.String())
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

	sessionURL, err := s.billingRepo.CreateCustomerSession(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to create customer session").Log(ctx, s.logger)
	}
	return sessionURL, nil
}
