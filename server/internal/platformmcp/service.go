package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/platform_mcp/server"
	gen "github.com/speakeasy-api/gram/server/gen/platform_mcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
)

type lifecycleStore interface {
	GetLifecycle(ctx context.Context, organizationID string) (Lifecycle, error)
	RevokeConnection(ctx context.Context, organizationID, connectionID string, now time.Time) error
}

type admissionEvaluator interface {
	Evaluate(ctx context.Context, organizationID, organizationSlug string) (Admission, error)
}

type Service struct {
	tracer     trace.Tracer
	logger     *slog.Logger
	auth       *auth.Auth
	authorizer Authorizer
	lifecycle  lifecycleStore
	admission  admissionEvaluator
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessionManager *sessions.Manager,
	authzEngine *authz.Engine,
	authorizer Authorizer,
	lifecycle lifecycleStore,
	admission admissionEvaluator,
) *Service {
	logger = logger.With(attr.SlogComponent("platformmcp"))
	return &Service{
		tracer:     tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/platformmcp"),
		logger:     logger,
		auth:       auth.New(logger, db, sessionManager, authzEngine),
		authorizer: authorizer,
		lifecycle:  lifecycle,
		admission:  admission,
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

func (s *Service) GetLifecycle(ctx context.Context, _ *gen.GetLifecyclePayload) (*gen.PlatformMCPLifecycle, error) {
	authCtx, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if s.lifecycle == nil {
		return nil, oops.C(oops.CodeUnexpected)
	}

	lifecycle, err := s.lifecycle.GetLifecycle(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, fmt.Errorf("get platform mcp lifecycle: %w", err)
	}

	admission := AdmissionIndeterminate
	if s.admission != nil {
		admission, err = s.admission.Evaluate(ctx, authCtx.ActiveOrganizationID, authCtx.OrganizationSlug)
		if err != nil {
			s.logger.WarnContext(ctx, "evaluate platform mcp lifecycle admission", attr.SlogError(err))
			admission = AdmissionIndeterminate
		}
	}

	return lifecycleResult(admission, lifecycle), nil
}

func (s *Service) RevokeConnection(ctx context.Context, payload *gen.RevokeConnectionPayload) error {
	authCtx, err := s.requireOrgAdmin(ctx)
	if err != nil {
		return err
	}
	if s.lifecycle == nil {
		return oops.C(oops.CodeUnexpected)
	}
	if err := s.lifecycle.RevokeConnection(ctx, authCtx.ActiveOrganizationID, payload.ConnectionID, time.Now()); err != nil {
		if errors.Is(err, platformoauth.ErrNotFound) || errors.Is(err, platformoauth.ErrRevoked) {
			return oops.C(oops.CodeNotFound)
		}
		return fmt.Errorf("revoke platform mcp connection: %w", err)
	}
	return nil
}

func (s *Service) requireOrgAdmin(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if s.authorizer == nil {
		return nil, oops.C(oops.CodeUnexpected)
	}
	if err := s.authorizer.RequireLiveOrgAdmin(ctx, Principal{
		UserID:         authCtx.UserID,
		OrganizationID: authCtx.ActiveOrganizationID,
		ConnectionID:   "",
		Generation:     "",
		ClientID:       "",
	}); err != nil {
		if isAuthorizationDenied(err) {
			return nil, oops.C(oops.CodeForbidden)
		}
		return nil, fmt.Errorf("require live organization admin: %w", err)
	}
	return authCtx, nil
}

func lifecycleResult(admission Admission, lifecycle Lifecycle) *gen.PlatformMCPLifecycle {
	connections := make([]*gen.PlatformMCPConnection, 0, len(lifecycle.Connections))
	ready := false
	for _, connection := range lifecycle.Connections {
		item := &gen.PlatformMCPConnection{
			ID:             connection.ID,
			Ready:          connection.Ready,
			AuthorizedAt:   nil,
			ReauthorizedAt: nil,
		}
		if connection.AuthorizedAt != nil {
			value := connection.AuthorizedAt.UTC().Format(time.RFC3339)
			item.AuthorizedAt = &value
		}
		if connection.ReauthorizedAt != nil {
			value := connection.ReauthorizedAt.UTC().Format(time.RFC3339)
			item.ReauthorizedAt = &value
		}
		connections = append(connections, item)
		ready = ready || connection.Ready
	}

	result := &gen.PlatformMCPLifecycle{
		Admission:            admissionString(admission),
		ReasonCode:           lifecycleReason(admission, lifecycle),
		DefaultProjectID:     nil,
		MarketplacePublished: lifecycle.MarketplacePublished,
		Connections:          connections,
		Authorized:           len(connections) > 0,
		Ready:                ready,
	}
	if lifecycle.DefaultProjectID != "" {
		result.DefaultProjectID = &lifecycle.DefaultProjectID
	}
	return result
}

func admissionString(admission Admission) string {
	switch admission {
	case AdmissionEnabled:
		return "enabled"
	case AdmissionDisabled:
		return "disabled"
	default:
		return "indeterminate"
	}
}

func lifecycleReason(admission Admission, lifecycle Lifecycle) string {
	for _, connection := range lifecycle.Connections {
		if connection.Ready {
			return "ready"
		}
	}
	if len(lifecycle.Connections) > 0 {
		return "authorized_awaiting_discovery"
	}

	switch {
	case lifecycle.DefaultProjectID == "":
		return "default_project_missing"
	case !lifecycle.MarketplacePublished:
		return "marketplace_unpublished"
	case admission == AdmissionIndeterminate:
		return "status_indeterminate"
	case admission == AdmissionDisabled:
		return "gate_disabled"
	default:
		return "eligible"
	}
}
