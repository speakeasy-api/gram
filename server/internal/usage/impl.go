package usage

import (
	"context"
	"fmt"
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
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/posthog"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
)

type Service struct {
	tracer        trace.Tracer
	logger        *slog.Logger
	auth          *auth.Auth
	authz         *authz.Engine
	serverURL     *url.URL
	repo          *repo.Queries
	billingRepo   billing.Repository
	orgRepo       *orgRepo.Queries
	posthogClient *posthog.Posthog
	openRouter    openrouter.Provisioner
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, billingRepo billing.Repository, serverURL *url.URL, posthogClient *posthog.Posthog, openRouter openrouter.Provisioner, authzEngine *authz.Engine) *Service {
	logger = logger.With(attr.SlogComponent("usage"))

	return &Service{
		tracer:        tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/usage"),
		logger:        logger,
		auth:          auth.New(logger, db, sessions, authzEngine),
		authz:         authzEngine,
		serverURL:     serverURL,
		repo:          repo.New(db),
		billingRepo:   billingRepo,
		orgRepo:       orgRepo.New(db),
		posthogClient: posthogClient,
		openRouter:    openRouter,
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
		"subscription.active",
		"subscription.canceled",
		"subscription.uncanceled",
		"subscription.revoked",
		"benefit_grant.created",
		"benefit_grant.cycled",
		"benefit_grant.updated",
		"benefit_grant.revoked",
		"order.paid",
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

	logger := s.logger.With(attr.SlogEvent(webhookPayload.Type))

	if !slices.Contains(acceptedEvents, webhookPayload.Type) {
		logger.InfoContext(ctx, "skipping unsupported webhook event")
		return nil
	}

	if webhookPayload.Data.Customer == nil || webhookPayload.Data.Customer.ExternalID == "" {
		logger.WarnContext(ctx, "skipping webhook: missing customer external id in webhook payload")
		return nil
	}

	if webhookPayload.Type == "order.paid" {
		if webhookPayload.Data.Product != nil && s.billingRepo.IsTopUpProductID(webhookPayload.Data.Product.ID) {
			return s.handleTopUpOrder(ctx, logger, webhookPayload)
		}
		logger.InfoContext(ctx, "skipping non-top-up order.paid; covered by subscription.* events")
		return nil
	}

	existingOrgMetadata, err := s.orgRepo.GetOrganizationMetadata(ctx, webhookPayload.Data.Customer.ExternalID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get organization metadata").Log(ctx, logger)
	}

	previousAccountType := existingOrgMetadata.GramAccountType

	// we must invalidate the customer tier cache since customer tier may have changed witha subscription update
	if err := s.billingRepo.InvalidateBillingCustomerCaches(ctx, webhookPayload.Data.Customer.ExternalID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to invalidate customer tier cache").Log(ctx, logger)
	}

	// we force a refresh of the state of the organization since customer tier may have changed witha subscription update
	refreshedOrg, err := mv.DescribeOrganization(ctx, s.logger, s.orgRepo, s.billingRepo, webhookPayload.Data.Customer.ExternalID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to update organization metadata").Log(ctx, logger)
	}
	updatedAccountType := refreshedOrg.GramAccountType

	// we must manually handle a downgrade from pro to free right now since there is no specific product subscription for free
	if previousAccountType == "pro" && webhookPayload.Type == "subscription.revoked" {
		updatedAccountType = "free"
		err := s.orgRepo.SetAccountType(ctx, orgRepo.SetAccountTypeParams{
			GramAccountType: updatedAccountType,
			ID:              refreshedOrg.ID,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to set account type").Log(ctx, logger)
		}
	}

	if previousAccountType != updatedAccountType {
		if _, err := s.openRouter.RefreshAPIKeyLimit(ctx, refreshedOrg.ID, nil); err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to refresh openrouter key limit").Log(ctx, logger)
		}
	}

	// we force a refresh of the period usage since usage may have changed with a subscription update
	if _, err = s.billingRepo.GetPeriodUsage(ctx, webhookPayload.Data.Customer.ExternalID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get period usage").Log(ctx, logger)
	}

	productName, productType := "", ""
	if webhookPayload.Data.Product != nil {
		productName = webhookPayload.Data.Product.Name
		productType = webhookPayload.Data.Product.Type
	}
	if err = s.posthogClient.CaptureEvent(ctx, "gram_subscription_changed", webhookPayload.Data.Customer.ExternalID, map[string]any{
		"org_id":       webhookPayload.Data.Customer.ExternalID,
		"org_name":     webhookPayload.Data.Customer.Name,
		"org_slug":     webhookPayload.Data.Customer.ExternalID,
		"is_gram":      true,
		"product":      productName,
		"product_type": productType,
		"event":        webhookPayload.Type,
		"email":        webhookPayload.Data.Customer.Email,
	}); err != nil {
		logger.ErrorContext(ctx, "failed to capture posthog event", attr.SlogError(err))
	}

	return nil
}

func (s *Service) handleTopUpOrder(ctx context.Context, logger *slog.Logger, webhookPayload *billing.PolarWebhookPayload) error {
	orgID := webhookPayload.Data.Customer.ExternalID

	if err := s.billingRepo.InvalidateBillingCustomerCaches(ctx, orgID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to invalidate customer tier cache").Log(ctx, logger)
	}
	if _, err := s.billingRepo.GetPeriodUsage(ctx, orgID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to get period usage").Log(ctx, logger)
	}

	if err := s.posthogClient.CaptureEvent(ctx, "gram_topup_purchased", orgID, map[string]any{
		"org_id":     orgID,
		"org_name":   webhookPayload.Data.Customer.Name,
		"org_slug":   orgID,
		"is_gram":    true,
		"product_id": webhookPayload.Data.Product.ID,
		"email":      webhookPayload.Data.Customer.Email,
	}); err != nil {
		logger.ErrorContext(ctx, "failed to capture posthog event", attr.SlogError(err))
	}

	return nil
}

func (s *Service) GetPeriodUsage(ctx context.Context, payload *gen.GetPeriodUsagePayload) (res *gen.PeriodUsage, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	// Prefer the cached period usage (populated hourly by the background worker and on
	// subscription changes via webhook). Only fall back to a live Polar fetch on cache miss
	// (new orgs that haven't been through a refresh cycle yet).
	periodUsage, err := s.billingRepo.GetStoredPeriodUsage(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		s.logger.InfoContext(ctx, "period usage cache miss, fetching from billing provider")
		periodUsage, err = s.billingRepo.GetPeriodUsage(ctx, authCtx.ActiveOrganizationID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "failed to get period usage").Log(ctx, s.logger)
		}
	}

	// The actual number of enabled servers right this moment, which may not be updated in Polar yet.
	actualEnabledServerCount, err := s.repo.GetEnabledServerCount(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "could not get public server count").Log(ctx, s.logger)
	}

	// We don't populate the maximums using GetUsageTiers because we want to reflect the actual granted credits, not the current product limits which may have changed.
	return &gen.PeriodUsage{
		ToolCalls:                periodUsage.ToolCalls,
		IncludedToolCalls:        periodUsage.IncludedToolCalls,
		Servers:                  periodUsage.Servers,
		IncludedServers:          periodUsage.IncludedServers,
		Credits:                  periodUsage.Credits,
		IncludedCredits:          periodUsage.IncludedCredits,
		HasActiveSubscription:    periodUsage.HasActiveSubscription,
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
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return "", err
	}

	successURL := fmt.Sprintf("%s/%s/billing", s.serverURL.String(), authCtx.OrganizationSlug)

	checkoutURL, err := s.billingRepo.CreateCheckout(ctx, authCtx.ActiveOrganizationID, s.serverURL.String(), successURL)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to create checkout").Log(ctx, s.logger)
	}
	return checkoutURL, nil
}

func (s *Service) CreateTopUpCheckout(ctx context.Context, payload *gen.CreateTopUpCheckoutPayload) (res string, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return "", oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return "", err
	}

	successURL := fmt.Sprintf("%s/%s/billing", s.serverURL.String(), authCtx.OrganizationSlug)

	checkoutURL, err := s.billingRepo.CreateTopUpCheckout(ctx, authCtx.ActiveOrganizationID, s.serverURL.String(), successURL)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to create top-up checkout").Log(ctx, s.logger)
	}
	return checkoutURL, nil
}

func (s *Service) CreateCustomerSession(ctx context.Context, payload *gen.CreateCustomerSessionPayload) (res string, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return "", oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return "", err
	}

	sessionURL, err := s.billingRepo.CreateCustomerSession(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "failed to create customer session").Log(ctx, s.logger)
	}
	return sessionURL, nil
}
