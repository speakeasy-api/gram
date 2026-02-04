package customdomains

import (
	"context"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/server/gen/domains"
	srv "github.com/speakeasy-api/gram/server/gen/http/domains/server"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
)

type Service struct {
	tracer         trace.Tracer
	logger         *slog.Logger
	repo           *repo.Queries
	auth           *auth.Auth
	temporalClient TemporalClient
}

type TemporalClient interface {
	GetWorkflowInfo(ctx context.Context, orgID string, domain string) (*workflowservice.DescribeWorkflowExecutionResponse, error)
	ExecuteCustomDomainRegistration(ctx context.Context, orgID string, domain string) (client.WorkflowRun, error)
	ExecuteCustomDomainDeletion(ctx context.Context, orgID, domain, ingressName, certSecretName string) (client.WorkflowRun, error)
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, temporal TemporalClient) *Service {
	logger = logger.With(attr.SlogComponent("customdomains"))

	return &Service{
		tracer:         otel.Tracer("github.com/speakeasy-api/gram/server/internal/customdomains"),
		logger:         logger,
		repo:           repo.New(db),
		auth:           auth.New(logger, db, sessions),
		temporalClient: temporal,
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
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	domain, err := s.repo.GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeNotFound, err, "no custom domain found for organization").Log(ctx, s.logger)
	}

	isUpdating := false
	if workflowInfo, _ := s.temporalClient.GetWorkflowInfo(ctx, authCtx.ActiveOrganizationID, domain.Domain); workflowInfo != nil {
		isUpdating = workflowInfo.GetWorkflowExecutionInfo().GetStatus() == enums.WORKFLOW_EXECUTION_STATUS_RUNNING
	}

	return &gen.CustomDomain{
		ID:             domain.ID.String(),
		OrganizationID: domain.OrganizationID,
		Domain:         domain.Domain,
		Verified:       domain.Verified,
		Activated:      domain.Activated,
		CreatedAt:      domain.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      domain.UpdatedAt.Time.Format(time.RFC3339),
		IsUpdating:     isUpdating,
	}, nil
}

func (s *Service) CreateDomain(ctx context.Context, payload *gen.CreateDomainPayload) (err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	if !slices.Contains([]string{"pro", "enterprise"}, authCtx.AccountType) {
		return oops.E(oops.CodeUnauthorized, err, "custom domain registration is not supported for free account").Log(ctx, s.logger)
	}

	_, err = s.temporalClient.ExecuteCustomDomainRegistration(ctx, authCtx.ActiveOrganizationID, payload.Domain)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error executing custom domain registration").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) DeleteDomain(ctx context.Context, _ *gen.DeleteDomainPayload) (err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return oops.C(oops.CodeUnauthorized)
	}

	domain, err := s.repo.GetCustomDomainByOrganization(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return oops.E(oops.CodeNotFound, err, "no custom domain found for organization").Log(ctx, s.logger)
	}

	if domain.Activated {
		run, err := s.temporalClient.ExecuteCustomDomainDeletion(ctx, authCtx.ActiveOrganizationID, domain.Domain, domain.IngressName.String, domain.CertSecretName.String)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "failed to start custom domain deletion workflow").Log(ctx, s.logger)
		}
		if err := run.Get(ctx, nil); err != nil {
			return oops.E(oops.CodeUnexpected, err, "custom domain deletion workflow failed").Log(ctx, s.logger)
		}
	}

	if err := s.repo.DeleteCustomDomain(ctx, authCtx.ActiveOrganizationID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete custom domain").Log(ctx, s.logger)
	}

	s.logger.InfoContext(ctx, "custom domain deleted",
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogURLDomain(domain.Domain),
	)

	return nil
}
