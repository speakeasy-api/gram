package assistants

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/assistants"
	srv "github.com/speakeasy-api/gram/server/gen/http/assistants/server"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	slackclient "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/client"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	auth   *auth.Auth
	authz  *authz.Engine
	core   *ServiceCore
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	assistantTokens *assistanttokens.Manager,
	serverURL *url.URL,
	slackClient *slackclient.SlackClient,
	runtimeConfig RuntimeBackendConfig,
	telemetryLogger *telemetry.Logger,
) (*Service, error) {
	logger = logger.With(attr.SlogComponent("assistants"))
	runtimeBackend, err := NewRuntimeBackend(logger, runtimeConfig)
	if err != nil {
		return nil, fmt.Errorf("create assistant runtime backend: %w", err)
	}
	if err := ValidateRuntimeBackendServerURL(context.Background(), runtimeBackend, serverURL); err != nil {
		return nil, err
	}
	instrumentedRuntime := newTelemetryRuntimeBackend(runtimeBackend, telemetryLogger)
	core := NewServiceCore(logger, db, instrumentedRuntime, slackClient, assistantTokens, serverURL, telemetryLogger)
	// Local Firecracker runtimes surface unexpected VM exits via callback so
	// the DB row can be reconciled; Fly machines are managed remotely and
	// don't expose an equivalent signal.
	if rm, ok := runtimeBackend.(*RuntimeManager); ok {
		rm.SetOnUnexpectedExit(core.HandleUnexpectedRuntimeExit)
	}
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/assistants"),
		logger: logger,
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
		core:   core,
	}, nil
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

func (s *Service) ListAssistants(ctx context.Context, _ *gen.ListAssistantsPayload) (*gen.ListAssistantsResult, error) {
	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
	}

	items, err := s.core.ListAssistants(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list assistants").Log(ctx, s.logger)
	}

	result := &gen.ListAssistantsResult{Assistants: make([]*types.Assistant, 0, len(items))}
	for _, item := range items {
		view, err := toHTTPAssistant(item)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build assistant view").Log(ctx, s.logger)
		}
		result.Assistants = append(result.Assistants, view)
	}
	return result, nil
}

func (s *Service) GetAssistant(ctx context.Context, payload *gen.GetAssistantPayload) (*types.Assistant, error) {
	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
	}
	assistantID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid assistant id").Log(ctx, s.logger)
	}

	record, err := s.core.GetAssistant(ctx, *authCtx.ProjectID, assistantID)
	if err != nil {
		return nil, mapAssistantStoreError(ctx, s.logger, err, "get assistant")
	}
	view, err := toHTTPAssistant(record)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build assistant view").Log(ctx, s.logger)
	}
	return view, nil
}

func (s *Service) CreateAssistant(ctx context.Context, payload *gen.CreateAssistantPayload) (*types.Assistant, error) {
	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
	}
	if authCtx.UserID == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "create assistant requires a user identity").Log(ctx, s.logger)
	}
	record, err := s.core.CreateAssistant(
		ctx,
		authCtx.ActiveOrganizationID,
		*authCtx.ProjectID,
		authCtx.UserID,
		payload.Name,
		payload.Model,
		payload.Instructions,
		payload.Toolsets,
		normalizeWarmTTLSeconds(payload.WarmTTLSeconds),
		normalizeMaxConcurrency(payload.MaxConcurrency),
		normalizeStatus(payload.Status),
	)
	if err != nil {
		return nil, mapAssistantStoreError(ctx, s.logger, err, "create assistant")
	}
	view, err := toHTTPAssistant(record)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build assistant view").Log(ctx, s.logger)
	}
	return view, nil
}

func (s *Service) UpdateAssistant(ctx context.Context, payload *gen.UpdateAssistantPayload) (*types.Assistant, error) {
	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
	}
	assistantID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid assistant id").Log(ctx, s.logger)
	}

	record, err := s.core.UpdateAssistant(
		ctx,
		*authCtx.ProjectID,
		assistantID,
		payload.Name,
		payload.Model,
		payload.Instructions,
		payload.Toolsets,
		payload.WarmTTLSeconds,
		payload.MaxConcurrency,
		payload.Status,
	)
	if err != nil {
		return nil, mapAssistantStoreError(ctx, s.logger, err, "update assistant")
	}
	view, err := toHTTPAssistant(record)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build assistant view").Log(ctx, s.logger)
	}
	return view, nil
}

func (s *Service) DeleteAssistant(ctx context.Context, payload *gen.DeleteAssistantPayload) error {
	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return err
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return err
	}
	assistantID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid assistant id").Log(ctx, s.logger)
	}
	if err := s.core.DeleteAssistant(ctx, *authCtx.ProjectID, assistantID); err != nil {
		return mapAssistantStoreError(ctx, s.logger, err, "delete assistant")
	}
	return nil
}

func (s *Service) Core() *ServiceCore {
	return s.core
}

func requireProjectAuthContext(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	return authCtx, nil
}

func mapAssistantStoreError(ctx context.Context, logger *slog.Logger, err error, message string) error {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return oops.E(oops.CodeNotFound, err, "%s", message).Log(ctx, logger)
	case errors.Is(err, errAssistantValidation):
		return oops.E(oops.CodeBadRequest, err, "%s", message).Log(ctx, logger)
	default:
		return oops.E(oops.CodeUnexpected, err, "%s", message).Log(ctx, logger)
	}
}
