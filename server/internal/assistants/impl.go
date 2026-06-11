package assistants

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type Service struct {
	tracer           trace.Tracer
	logger           *slog.Logger
	auth             *auth.Auth
	authz            *authz.Engine
	core             *ServiceCore
	signaler         WorkflowSignaler
	bootstrapLimiter *assistantRateLimiter
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)
var _ bgtriggers.Dispatcher = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	core *ServiceCore,
	signaler WorkflowSignaler,
) *Service {
	logger = logger.With(attr.SlogComponent("assistants"))
	return &Service{
		tracer:           tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/assistants"),
		logger:           logger,
		auth:             auth.New(logger, db, sessions, authzEngine),
		authz:            authzEngine,
		core:             core,
		signaler:         signaler,
		bootstrapLimiter: newAssistantRateLimiter(),
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
	o11y.AttachHandler(mux, "POST", "/rpc/assistants.getThreadBootstrap", oops.ErrHandle(service.logger, service.handleGetThreadBootstrap).ServeHTTP)
	o11y.AttachHandler(mux, "POST", "/rpc/assistantMcpAuth.create", oops.ErrHandle(service.logger, service.handleCreateMCPAuthFlow).ServeHTTP)
	o11y.AttachHandler(mux, "GET", "/rpc/assistantMcpAuth/{id}/oauth/callback", oops.ErrHandle(service.logger, service.handleMCPAuthCallback).ServeHTTP)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) ListAssistants(ctx context.Context, _ *gen.ListAssistantsPayload) (*gen.ListAssistantsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
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
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
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
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
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
		conv.PtrValOrEmpty(payload.Status, StatusActive),
	)
	if err != nil {
		return nil, mapAssistantStoreError(ctx, s.logger, err, "create assistant")
	}
	s.startRuntimeWarmup(ctx, record)
	view, err := toHTTPAssistant(record)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build assistant view").Log(ctx, s.logger)
	}
	return view, nil
}

func (s *Service) UpdateAssistant(ctx context.Context, payload *gen.UpdateAssistantPayload) (*types.Assistant, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
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
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
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

func (s *Service) SendMessage(ctx context.Context, payload *gen.SendMessagePayload) (*gen.SendMessageResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	// Messages are sent as the calling user, so a user identity is required.
	if authCtx.UserID == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "sending a message requires a user identity").Log(ctx, s.logger)
	}

	assistantID, err := uuid.Parse(payload.AssistantID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid assistant id").Log(ctx, s.logger)
	}

	// chat_id names the conversation to continue; omit it to start a new one (the
	// server mints and returns a fresh id).
	var chatID uuid.UUID
	if payload.ChatID != nil && *payload.ChatID != "" {
		chatID, err = uuid.Parse(*payload.ChatID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid chat id").Log(ctx, s.logger)
		}
	}

	// Continuing a conversation requires the caller to own it: chat ids aren't
	// user-namespaced, so without this gate one user could pin their next turn
	// onto another user's chat. uuid.Nil mints a fresh chat, so skip the check.
	// A miss is surfaced as not-found to avoid disclosing chat existence.
	if chatID != uuid.Nil {
		if err := s.core.CheckDashboardChatOwnership(ctx, *authCtx.ProjectID, chatID, authCtx.UserID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "chat not found").Log(ctx, s.logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "resolve chat access").Log(ctx, s.logger)
		}
	}

	idempotencyKey := ""
	if payload.IdempotencyKey != nil {
		idempotencyKey = *payload.IdempotencyKey
	}

	result, err := s.core.SendDashboardMessage(ctx, *authCtx.ProjectID, assistantID, authCtx.UserID, chatID, payload.Message, idempotencyKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "assistant not found").Log(ctx, s.logger)
		}
		return nil, mapAssistantStoreError(ctx, s.logger, err, "send assistant message")
	}

	var threadID *string
	if result.ThreadID != uuid.Nil {
		threadID = new(result.ThreadID.String())
	}
	return &gen.SendMessageResult{
		ChatID:   result.ChatID.String(),
		ThreadID: threadID,
		Accepted: result.Accepted,
	}, nil
}

func (s *Service) EnsureManagedAssistant(ctx context.Context, _ *gen.EnsureManagedAssistantPayload) (*types.Assistant, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}
	// Provisioning records the creating user, so a user identity is required.
	if authCtx.UserID == "" {
		return nil, oops.E(oops.CodeUnauthorized, nil, "the project assistant requires a user identity").Log(ctx, s.logger)
	}

	record, err := s.core.EnableManagedAssistant(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, authCtx.UserID)
	if err != nil {
		if errors.Is(err, ErrManagedAssistantNameTaken) {
			return nil, oops.E(oops.CodeConflict, err, "an assistant with the project assistant's name already exists — rename it to enable the built-in assistant").Log(ctx, s.logger)
		}
		return nil, mapAssistantStoreError(ctx, s.logger, err, "ensure project assistant")
	}
	s.startRuntimeWarmup(ctx, record)
	view, err := toHTTPAssistant(record)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build assistant view").Log(ctx, s.logger)
	}
	return view, nil
}

func (s *Service) Kind() string {
	return bgtriggers.TargetKindAssistant
}

func (s *Service) Dispatch(ctx context.Context, task bgtriggers.Task) error {
	result, err := s.core.EnqueueTriggerTask(ctx, task)
	if err != nil {
		return fmt.Errorf("enqueue assistant trigger task: %w", err)
	}
	if !result.ShouldSignal || result.AssistantID == uuid.Nil {
		return nil
	}
	if err := s.signaler.SignalCoordinator(ctx, result.AssistantID); err != nil {
		return fmt.Errorf("signal assistant coordinator: %w", err)
	}
	return nil
}

// startRuntimeWarmup eagerly boots the assistant's runtime so the first turn
// doesn't pay the Fly cold-start cost. It rides the standard turn machinery:
// an event-less warmup thread reserves the runtime row, and its thread
// workflow runs Ensure, coordinator kicks and the warm window exactly as a
// turn would. Best-effort: a failure here never fails the request — the
// first turn boots lazily instead.
func (s *Service) startRuntimeWarmup(ctx context.Context, record assistantRecord) {
	if record.Status != StatusActive {
		return
	}
	result, err := s.core.EnsureWarmupThread(ctx, record.ID)
	if err != nil {
		s.logger.WarnContext(ctx, "ensure assistant warmup thread failed",
			attr.SlogAssistantID(record.ID.String()),
			attr.SlogError(err),
		)
		return
	}
	if !result.ShouldSignal {
		return
	}
	if err := s.signaler.SignalThread(ctx, result.ThreadID, result.ProjectID); err != nil {
		s.logger.WarnContext(ctx, "signal assistant warmup thread failed",
			attr.SlogAssistantID(record.ID.String()),
			attr.SlogError(err),
		)
		// Nothing drives a reserved row whose signal was lost, and admit
		// holds real turns back while it sits in `starting` — release it so
		// the first turn boots lazily instead of waiting out the reaper.
		s.core.ReleaseWarmupRuntime(ctx, record.ProjectID, record.ID, result.ThreadID)
	}
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
