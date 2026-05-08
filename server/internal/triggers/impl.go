package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/triggers/server"
	gen "github.com/speakeasy-api/gram/server/gen/triggers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	auth   *auth.Auth
	app    *bgtriggers.App
	audit  *audit.Logger
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	app *bgtriggers.App,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("triggers"))
	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/triggers"),
		logger: logger,
		auth:   auth.New(logger, db, sessions, authzEngine),
		app:    app,
		audit:  auditLogger,
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

	o11y.AttachHandler(mux, "POST", "/rpc/triggers/{id}/webhook", func(w http.ResponseWriter, r *http.Request) {
		oops.ErrHandle(service.logger, service.HandleWebhook).ServeHTTP(w, r)
	})
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) ListTriggerDefinitions(ctx context.Context, _ *gen.ListTriggerDefinitionsPayload) (*gen.ListTriggerDefinitionsResult, error) {
	definitions := bgtriggers.List()
	result := make([]*types.TriggerDefinition, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, buildDefinitionView(definition))
	}
	return &gen.ListTriggerDefinitionsResult{Definitions: result}, nil
}

func (s *Service) ListTriggerInstances(ctx context.Context, _ *gen.ListTriggerInstancesPayload) (*gen.ListTriggerInstancesResult, error) {
	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	items, err := s.app.ListInstances(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, toTriggerError(ctx, s.logger, err, "list trigger instances")
	}

	result := make([]*types.TriggerInstance, 0, len(items))
	for _, item := range items {
		view, err := buildTriggerView(item, s.app.WebhookURL(item))
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build trigger instance view").Log(ctx, s.logger)
		}
		result = append(result, view)
	}

	return &gen.ListTriggerInstancesResult{Triggers: result}, nil
}

func (s *Service) GetTriggerInstance(ctx context.Context, payload *gen.GetTriggerInstancePayload) (*types.TriggerInstance, error) {
	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	triggerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid trigger id").Log(ctx, s.logger)
	}

	item, err := s.app.GetInstance(ctx, *authCtx.ProjectID, triggerID)
	if err != nil {
		return nil, toTriggerError(ctx, s.logger, err, "get trigger instance")
	}

	view, err := buildTriggerView(item, s.app.WebhookURL(item))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build trigger instance view").Log(ctx, s.logger)
	}
	return view, nil
}

func (s *Service) CreateTriggerInstance(ctx context.Context, payload *gen.CreateTriggerInstancePayload) (*types.TriggerInstance, error) {
	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	var envID uuid.NullUUID
	if payload.EnvironmentID != nil {
		parsed, err := uuid.Parse(*payload.EnvironmentID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid environment id").Log(ctx, s.logger)
		}
		envID = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	item, err := s.app.Create(ctx, bgtriggers.CreateParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		DefinitionSlug: payload.DefinitionSlug,
		Name:           payload.Name,
		EnvironmentID:  envID,
		TargetKind:     payload.TargetKind,
		TargetRef:      payload.TargetRef,
		TargetDisplay:  payload.TargetDisplay,
		Config:         payload.Config,
		Status:         normalizeTriggerStatus(payload.Status),
	}, func(ctx context.Context, dbtx pgx.Tx, instance triggerrepo.TriggerInstance) error {
		return s.audit.LogTriggerInstanceCreate(ctx, dbtx, audit.LogTriggerInstanceCreateEvent{
			OrganizationID:     authCtx.ActiveOrganizationID,
			ProjectID:          *authCtx.ProjectID,
			Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:   authCtx.Email,
			ActorSlug:          nil,
			TriggerInstanceURN: urn.NewTriggerInstance(instance.ID),
			Name:               instance.Name,
			DefinitionSlug:     instance.DefinitionSlug,
		})
	})
	if err != nil {
		return nil, toTriggerError(ctx, s.logger, err, "create trigger instance")
	}

	view, err := buildTriggerView(item, s.app.WebhookURL(item))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build trigger instance view").Log(ctx, s.logger)
	}
	return view, nil
}

func (s *Service) UpdateTriggerInstance(ctx context.Context, payload *gen.UpdateTriggerInstancePayload) (*types.TriggerInstance, error) {
	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	triggerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid trigger id").Log(ctx, s.logger)
	}

	existing, err := s.app.GetInstance(ctx, *authCtx.ProjectID, triggerID)
	if err != nil {
		return nil, toTriggerError(ctx, s.logger, err, "get trigger instance")
	}

	environmentID := nullUUIDToUUID(existing.EnvironmentID)
	if payload.EnvironmentID != nil {
		environmentID, err = uuid.Parse(*payload.EnvironmentID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid environment id").Log(ctx, s.logger)
		}
	}

	configMap, err := configJSONToMap(existing.ConfigJson)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decode trigger config").Log(ctx, s.logger)
	}
	if payload.Config != nil {
		configMap = payload.Config
	}

	beforeView, err := buildTriggerView(existing, s.app.WebhookURL(existing))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build trigger instance view").Log(ctx, s.logger)
	}

	item, err := s.app.Update(ctx, bgtriggers.UpdateParams{
		ID:             triggerID,
		ProjectID:      *authCtx.ProjectID,
		DefinitionSlug: existing.DefinitionSlug,
		Name:           valueOrDefault(payload.Name, existing.Name),
		EnvironmentID:  uuid.NullUUID{UUID: environmentID, Valid: environmentID != uuid.Nil},
		TargetKind:     valueOrDefault(payload.TargetKind, existing.TargetKind),
		TargetRef:      valueOrDefault(payload.TargetRef, existing.TargetRef),
		TargetDisplay:  valueOrDefault(payload.TargetDisplay, existing.TargetDisplay),
		Config:         configMap,
		Status:         valueOrDefault(payload.Status, existing.Status),
	}, func(ctx context.Context, dbtx pgx.Tx, instance triggerrepo.TriggerInstance) error {
		afterView, err := buildTriggerView(instance, s.app.WebhookURL(instance))
		if err != nil {
			return fmt.Errorf("build trigger instance after-snapshot: %w", err)
		}
		return s.audit.LogTriggerInstanceUpdate(ctx, dbtx, audit.LogTriggerInstanceUpdateEvent{
			OrganizationID:                authCtx.ActiveOrganizationID,
			ProjectID:                     *authCtx.ProjectID,
			Actor:                         urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:              authCtx.Email,
			ActorSlug:                     nil,
			TriggerInstanceURN:            urn.NewTriggerInstance(instance.ID),
			Name:                          instance.Name,
			DefinitionSlug:                instance.DefinitionSlug,
			TriggerInstanceSnapshotBefore: beforeView,
			TriggerInstanceSnapshotAfter:  afterView,
		})
	})
	if err != nil {
		return nil, toTriggerError(ctx, s.logger, err, "update trigger instance")
	}

	view, err := buildTriggerView(item, s.app.WebhookURL(item))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build trigger instance view").Log(ctx, s.logger)
	}
	return view, nil
}

func (s *Service) DeleteTriggerInstance(ctx context.Context, payload *gen.DeleteTriggerInstancePayload) error {
	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return err
	}

	triggerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid trigger id").Log(ctx, s.logger)
	}

	if err := s.app.Delete(ctx, *authCtx.ProjectID, triggerID, func(ctx context.Context, dbtx pgx.Tx, instance triggerrepo.TriggerInstance) error {
		return s.audit.LogTriggerInstanceDelete(ctx, dbtx, audit.LogTriggerInstanceDeleteEvent{
			OrganizationID:     authCtx.ActiveOrganizationID,
			ProjectID:          *authCtx.ProjectID,
			Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:   authCtx.Email,
			ActorSlug:          nil,
			TriggerInstanceURN: urn.NewTriggerInstance(instance.ID),
			Name:               instance.Name,
			DefinitionSlug:     instance.DefinitionSlug,
		})
	}); err != nil {
		return toTriggerError(ctx, s.logger, err, "delete trigger instance")
	}

	return nil
}

func (s *Service) PauseTriggerInstance(ctx context.Context, payload *gen.PauseTriggerInstancePayload) (*types.TriggerInstance, error) {
	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.setTriggerStatus(ctx, authCtx, payload.ID, bgtriggers.StatusPaused, func(ctx context.Context, dbtx pgx.Tx, instance triggerrepo.TriggerInstance) error {
		return s.audit.LogTriggerInstancePause(ctx, dbtx, audit.LogTriggerInstancePauseEvent{
			OrganizationID:     authCtx.ActiveOrganizationID,
			ProjectID:          *authCtx.ProjectID,
			Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:   authCtx.Email,
			ActorSlug:          nil,
			TriggerInstanceURN: urn.NewTriggerInstance(instance.ID),
			Name:               instance.Name,
			DefinitionSlug:     instance.DefinitionSlug,
		})
	})
}

func (s *Service) ResumeTriggerInstance(ctx context.Context, payload *gen.ResumeTriggerInstancePayload) (*types.TriggerInstance, error) {
	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return nil, err
	}

	return s.setTriggerStatus(ctx, authCtx, payload.ID, bgtriggers.StatusActive, func(ctx context.Context, dbtx pgx.Tx, instance triggerrepo.TriggerInstance) error {
		return s.audit.LogTriggerInstanceResume(ctx, dbtx, audit.LogTriggerInstanceResumeEvent{
			OrganizationID:     authCtx.ActiveOrganizationID,
			ProjectID:          *authCtx.ProjectID,
			Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:   authCtx.Email,
			ActorSlug:          nil,
			TriggerInstanceURN: urn.NewTriggerInstance(instance.ID),
			Name:               instance.Name,
			DefinitionSlug:     instance.DefinitionSlug,
		})
	})
}

func (s *Service) setTriggerStatus(ctx context.Context, authCtx *contextvalues.AuthContext, id string, status string, hooks ...bgtriggers.InstanceDBHook) (*types.TriggerInstance, error) {
	triggerID, err := uuid.Parse(id)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid trigger id").Log(ctx, s.logger)
	}

	item, err := s.app.SetStatus(ctx, *authCtx.ProjectID, triggerID, status, hooks...)
	if err != nil {
		return nil, toTriggerError(ctx, s.logger, err, "set trigger status")
	}

	view, err := buildTriggerView(item, s.app.WebhookURL(item))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build trigger instance view").Log(ctx, s.logger)
	}
	return view, nil
}

// HandleWebhook takes an incoming webhook request and passes it onto the trigger app facade
//
// NOTE(security): webhook signature is checked in `*App.ProcessWebhook`. Requires
// `Definition.AuthenticateWebhook` to be defined.
func (s *Service) HandleWebhook(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	triggerID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid trigger id").Log(ctx, s.logger)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "read request body").Log(ctx, s.logger)
	}

	result, err := s.app.ProcessWebhook(ctx, triggerID, body, r.Header)
	if err != nil {
		return toTriggerError(ctx, s.logger, err, "process trigger webhook")
	}

	statusCode := http.StatusOK
	contentType := "application/json"
	responseBody := []byte(`{"ok":true}`)
	if result != nil && result.Response != nil {
		statusCode = result.Response.Status
		contentType = result.Response.ContentType
		responseBody = result.Response.Body
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(statusCode)
	if _, err := w.Write(responseBody); err != nil {
		return oops.E(oops.CodeUnexpected, err, "write webhook response").Log(ctx, s.logger)
	}

	return nil
}

func requireProjectAuthContext(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	return authCtx, nil
}

func normalizeTriggerStatus(status *string) string {
	if status == nil || *status == "" {
		return bgtriggers.StatusActive
	}
	return *status
}

func configJSONToMap(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("unmarshal config json: %w", err)
	}
	if config == nil {
		return map[string]any{}, nil
	}
	return config, nil
}

func buildTriggerView(instance triggerrepo.TriggerInstance, webhookURL *string) (*types.TriggerInstance, error) {
	view, err := mv.BuildTriggerInstanceView(instance, webhookURL)
	if err != nil {
		return nil, fmt.Errorf("build trigger instance view: %w", err)
	}
	return view, nil
}

func buildDefinitionView(definition bgtriggers.Definition) *types.TriggerDefinition {
	requirements := make([]*types.TriggerEnvRequirement, 0, len(definition.EnvRequirements))
	for _, requirement := range definition.EnvRequirements {
		requirements = append(requirements, &types.TriggerEnvRequirement{
			Name:        requirement.Name,
			Description: conv.PtrEmpty(requirement.Description),
			Required:    requirement.Required,
		})
	}
	return &types.TriggerDefinition{
		Slug:            definition.Slug,
		Title:           definition.Title,
		Description:     definition.Description,
		Kind:            string(definition.Kind),
		ConfigSchema:    string(definition.ConfigSchema),
		EnvRequirements: requirements,
	}
}

func toTriggerError(ctx context.Context, logger *slog.Logger, err error, message string) error {
	code := oops.CodeUnexpected
	public := message
	switch {
	case errors.Is(err, bgtriggers.ErrBadRequest):
		code = oops.CodeBadRequest
		// Surface the validation detail (e.g. JSON schema mismatch on
		// trigger config) so callers — especially LLM-driven ones — can
		// self-correct. The chain is already user-actionable: it's only
		// reached when the input fails validation.
		public = fmt.Sprintf("%s: %s", message, err.Error())
	case errors.Is(err, bgtriggers.ErrAuthFailed):
		code = oops.CodeUnauthorized
	case errors.Is(err, pgx.ErrNoRows):
		code = oops.CodeNotFound
	}
	return oops.E(code, err, "%s", public).Log(ctx, logger)
}

func nullUUIDToUUID(value uuid.NullUUID) uuid.UUID {
	if !value.Valid {
		return uuid.Nil
	}
	return value.UUID
}

func valueOrDefault(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}
