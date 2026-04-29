package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

const (
	TargetKindAssistant = "assistant"
	TargetKindNoop      = "noop"
	StatusActive        = "active"
	StatusPaused        = "paused"
)

var ErrBadRequest = errors.New("trigger bad request")

type DeliveryStatus string

const (
	DeliveryStatusFailed  DeliveryStatus = "failed"
	DeliveryStatusSent    DeliveryStatus = "sent"
	DeliveryStatusSkipped DeliveryStatus = "skipped"
)

type EnvironmentLoader interface {
	Load(context.Context, uuid.UUID, toolconfig.SlugOrID) (map[string]string, error)
}

type DeliveryLogger interface {
	LogTriggerDelivery(triggerrepo.TriggerInstance, EventEnvelope, DeliveryStatus, string, error)
}

type Dispatcher interface {
	Kind() string
	Dispatch(context.Context, Task) error
}

type TriggerDeliveryLog struct {
	Timestamp  time.Time
	Instance   triggerrepo.TriggerInstance
	Attributes map[attr.Key]any
}

type triggerDeliveryLogger struct {
	write func(context.Context, TriggerDeliveryLog)
}

func NewTriggerDeliveryLogger(write func(context.Context, TriggerDeliveryLog)) DeliveryLogger {
	return &triggerDeliveryLogger{write: write}
}

func (l *triggerDeliveryLogger) LogTriggerDelivery(
	instance triggerrepo.TriggerInstance,
	envelope EventEnvelope,
	status DeliveryStatus,
	reason string,
	err error,
) {
	if l == nil || l.write == nil {
		return
	}

	body := fmt.Sprintf("trigger event %s", status)
	if reason != "" {
		body += ": " + reason
	}

	attributes := map[attr.Key]any{
		attr.TriggerDefinitionSlugKey: instance.DefinitionSlug,
		attr.TriggerInstanceIDKey:     instance.ID.String(),
		attr.TriggerEventIDKey:        envelope.EventID,
		attr.TriggerCorrelationIDKey:  envelope.CorrelationID,
		attr.TriggerDeliveryStatusKey: string(status),
		attr.TriggerTargetKindKey:     instance.TargetKind,
		attr.TriggerTargetRefKey:      instance.TargetRef,
		attr.LogBodyKey:               body,
		attr.LogSeverityKey:           conv.Ternary(status == DeliveryStatusFailed, "ERROR", "INFO"),
	}
	if reason != "" {
		attributes[attr.ReasonKey] = reason
	}
	if instance.EnvironmentID.Valid {
		attributes[attr.EnvironmentIDKey] = instance.EnvironmentID.UUID.String()
	}
	if err != nil {
		attributes[attr.ErrorMessageKey] = err.Error()
	}

	l.write(context.Background(), TriggerDeliveryLog{
		Timestamp:  conv.Default(envelope.ReceivedAt, time.Now().UTC()),
		Attributes: attributes,
		Instance:   instance,
	})
}

type App struct {
	logger         *slog.Logger
	repo           *triggerrepo.Queries
	envLoader      EnvironmentLoader
	deliveryLogger DeliveryLogger
	temporalEnv    *tenv.Environment
	serverURL      *url.URL
	dispatchers    map[string]Dispatcher
}

type CreateParams struct {
	OrganizationID string
	ProjectID      uuid.UUID
	DefinitionSlug string
	Name           string
	EnvironmentID  uuid.NullUUID
	TargetKind     string
	TargetRef      string
	TargetDisplay  string
	Config         map[string]any
	Status         string
}

type UpdateParams struct {
	ID             uuid.UUID
	ProjectID      uuid.UUID
	DefinitionSlug string
	Name           string
	EnvironmentID  uuid.NullUUID
	TargetKind     string
	TargetRef      string
	TargetDisplay  string
	Config         map[string]any
	Status         string
}

type ProcessScheduledInput struct {
	TriggerInstanceID string
	FiredAt           string
}

func NewApp(
	logger *slog.Logger,
	db *pgxpool.Pool,
	temporalEnv *tenv.Environment,
	envLoader EnvironmentLoader,
	deliveryLogger DeliveryLogger,
	serverURL *url.URL,
	dispatchers ...Dispatcher,
) *App {
	logger = logger.With(attr.SlogComponent("background_triggers"))

	dispatcherMap := make(map[string]Dispatcher, len(dispatchers))
	for _, dispatcher := range dispatchers {
		dispatcherMap[dispatcher.Kind()] = dispatcher
	}

	return &App{
		logger:         logger,
		repo:           triggerrepo.New(db),
		envLoader:      envLoader,
		deliveryLogger: deliveryLogger,
		temporalEnv:    temporalEnv,
		serverURL:      serverURL,
		dispatchers:    dispatcherMap,
	}
}

func (a *App) ListDefinitions() []Definition {
	return List()
}

func (a *App) ListInstances(ctx context.Context, projectID uuid.UUID) ([]triggerrepo.TriggerInstance, error) {
	items, err := a.repo.ListTriggerInstances(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list trigger instances: %w", err)
	}
	return items, nil
}

func (a *App) GetInstance(ctx context.Context, projectID uuid.UUID, id uuid.UUID) (triggerrepo.TriggerInstance, error) {
	item, err := a.repo.GetTriggerInstanceByID(ctx, triggerrepo.GetTriggerInstanceByIDParams{
		ID:        id,
		ProjectID: projectID,
	})
	if err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("get trigger instance: %w", err)
	}
	return item, nil
}

func (a *App) Create(ctx context.Context, params CreateParams) (triggerrepo.TriggerInstance, error) {
	if err := ValidateTargetKind(params.TargetKind); err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("%w: %w", ErrBadRequest, err)
	}
	if params.Config == nil {
		params.Config = map[string]any{}
	}

	definition, config, err := a.validateInstance(ctx, params.ProjectID, nullUUIDToUUID(params.EnvironmentID), params.DefinitionSlug, params.Config)
	if err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("%w: validate trigger instance: %w", ErrBadRequest, err)
	}

	configJSON, err := marshalConfigJSON(params.Config)
	if err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("marshal trigger config: %w", err)
	}

	item, err := a.repo.CreateTriggerInstance(ctx, triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: params.OrganizationID,
		ProjectID:      params.ProjectID,
		DefinitionSlug: params.DefinitionSlug,
		Name:           params.Name,
		EnvironmentID:  params.EnvironmentID,
		TargetKind:     params.TargetKind,
		TargetRef:      params.TargetRef,
		TargetDisplay:  params.TargetDisplay,
		ConfigJson:     configJSON,
		Status:         params.Status,
	})
	if err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("create trigger instance: %w", err)
	}

	if err := a.reconcileSchedule(ctx, item, definition, config); err != nil {
		_, _ = a.repo.DeleteTriggerInstance(ctx, triggerrepo.DeleteTriggerInstanceParams{
			ID:        item.ID,
			ProjectID: params.ProjectID,
		})
		return triggerrepo.TriggerInstance{}, err
	}

	return item, nil
}

func (a *App) Update(ctx context.Context, params UpdateParams) (triggerrepo.TriggerInstance, error) {
	if err := ValidateTargetKind(params.TargetKind); err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("%w: %w", ErrBadRequest, err)
	}
	if params.Config == nil {
		params.Config = map[string]any{}
	}

	existing, err := a.GetInstance(ctx, params.ProjectID, params.ID)
	if err != nil {
		return triggerrepo.TriggerInstance{}, err
	}
	if existing.DefinitionSlug != params.DefinitionSlug {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("trigger %s is %q, expected %q", existing.ID.String(), existing.DefinitionSlug, params.DefinitionSlug)
	}

	definition, config, err := a.validateInstance(ctx, params.ProjectID, nullUUIDToUUID(params.EnvironmentID), params.DefinitionSlug, params.Config)
	if err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("%w: validate trigger instance: %w", ErrBadRequest, err)
	}

	configJSON, err := marshalConfigJSON(params.Config)
	if err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("marshal trigger config: %w", err)
	}

	item, err := a.repo.UpdateTriggerInstance(ctx, triggerrepo.UpdateTriggerInstanceParams{
		Name:                conv.ToPGText(params.Name),
		UpdateEnvironmentID: true,
		EnvironmentID:       params.EnvironmentID,
		TargetKind:          conv.ToPGText(params.TargetKind),
		TargetRef:           conv.ToPGText(params.TargetRef),
		TargetDisplay:       conv.ToPGText(params.TargetDisplay),
		ConfigJson:          configJSON,
		Status:              conv.ToPGText(params.Status),
		ID:                  params.ID,
		ProjectID:           params.ProjectID,
	})
	if err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("update trigger instance: %w", err)
	}

	if err := a.reconcileSchedule(ctx, item, definition, config); err != nil {
		return triggerrepo.TriggerInstance{}, err
	}

	return item, nil
}

func (a *App) Delete(ctx context.Context, projectID uuid.UUID, id uuid.UUID) error {
	item, err := a.repo.DeleteTriggerInstance(ctx, triggerrepo.DeleteTriggerInstanceParams{
		ID:        id,
		ProjectID: projectID,
	})
	if err != nil {
		return fmt.Errorf("delete trigger instance: %w", err)
	}

	if err := a.deleteSchedule(ctx, item); err != nil {
		return err
	}

	return nil
}

func (a *App) SetStatus(ctx context.Context, projectID uuid.UUID, id uuid.UUID, status string) (triggerrepo.TriggerInstance, error) {
	item, err := a.repo.SetTriggerInstanceStatus(ctx, triggerrepo.SetTriggerInstanceStatusParams{
		Status:    status,
		ID:        id,
		ProjectID: projectID,
	})
	if err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("set trigger status: %w", err)
	}

	rawConfig, err := configJSONToMap(item.ConfigJson)
	if err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("decode trigger config: %w", err)
	}
	definition, config, err := a.validateInstance(ctx, item.ProjectID, nullUUIDToUUID(item.EnvironmentID), item.DefinitionSlug, rawConfig)
	if err != nil {
		return triggerrepo.TriggerInstance{}, fmt.Errorf("%w: validate trigger instance: %w", ErrBadRequest, err)
	}
	if err := a.reconcileSchedule(ctx, item, definition, config); err != nil {
		return triggerrepo.TriggerInstance{}, err
	}

	return item, nil
}

func (a *App) ProcessWebhook(ctx context.Context, instanceID uuid.UUID, body []byte, headers http.Header) (*WebhookIngressResult, error) {
	instance, err := a.repo.GetTriggerInstanceByIDPublic(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get trigger instance: %w", err)
	}

	definition, ok := GetDefinition(instance.DefinitionSlug)
	if !ok {
		return nil, fmt.Errorf("unknown trigger definition %q", instance.DefinitionSlug)
	}
	if definition.Kind != KindWebhook {
		return nil, fmt.Errorf("trigger definition %q is not webhook-backed", instance.DefinitionSlug)
	}
	if definition.AuthenticateWebhook == nil {
		return nil, fmt.Errorf("trigger definition %q does not implement webhook authentication", instance.DefinitionSlug)
	}
	if definition.HandleWebhook == nil {
		return nil, fmt.Errorf("trigger definition %q does not implement webhook ingress", instance.DefinitionSlug)
	}

	rawConfig, err := configJSONToMap(instance.ConfigJson)
	if err != nil {
		return nil, fmt.Errorf("decode trigger config: %w", err)
	}
	_, config, err := a.validateInstance(ctx, instance.ProjectID, nullUUIDToUUID(instance.EnvironmentID), instance.DefinitionSlug, rawConfig)
	if err != nil {
		return nil, fmt.Errorf("validate trigger instance: %w", err)
	}

	envMap := map[string]string{}
	if instance.EnvironmentID.Valid {
		envMap, err = a.loadEnvironmentMap(ctx, instance.ProjectID, instance.EnvironmentID.UUID)
		if err != nil {
			return nil, fmt.Errorf("load environment: %w", err)
		}
	}

	if err := definition.AuthenticateWebhook(body, headers, envMap, config); err != nil {
		return nil, fmt.Errorf("authenticate webhook: %w", err)
	}

	result, err := definition.HandleWebhook(body, headers, config)
	if err != nil {
		return nil, fmt.Errorf("handle webhook: %w", err)
	}
	if result == nil {
		return nil, nil
	}
	if result.Event == nil {
		return result, nil
	}

	result.Event.TriggerInstanceID = instance.ID.String()
	result.Event.DefinitionSlug = instance.DefinitionSlug

	task, err := a.ProcessEvent(ctx, instance, *result.Event)
	if err != nil {
		return nil, fmt.Errorf("process event: %w", err)
	}
	result.Task = task
	if task == nil {
		return result, nil
	}

	if err := ExecuteTriggerDispatchWorkflow(ctx, a.temporalEnv, TriggerDispatchWorkflowInput{Task: *task}); err != nil {
		return nil, fmt.Errorf("execute trigger dispatch workflow: %w", err)
	}

	return result, nil
}

func (a *App) ProcessScheduled(ctx context.Context, input ProcessScheduledInput) (*Task, error) {
	instanceID, err := uuid.Parse(input.TriggerInstanceID)
	if err != nil {
		return nil, fmt.Errorf("parse trigger instance id: %w", err)
	}

	instance, err := a.repo.GetTriggerInstanceByIDPublic(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get trigger instance: %w", err)
	}

	rawConfig, err := configJSONToMap(instance.ConfigJson)
	if err != nil {
		return nil, fmt.Errorf("decode trigger config: %w", err)
	}

	definition, config, err := a.validateInstance(ctx, instance.ProjectID, nullUUIDToUUID(instance.EnvironmentID), instance.DefinitionSlug, rawConfig)
	if err != nil {
		return nil, fmt.Errorf("validate trigger instance: %w", err)
	}
	if definition.BuildScheduledEvent == nil {
		return nil, fmt.Errorf("trigger definition %q does not implement scheduled ingress", instance.DefinitionSlug)
	}

	firedAt, err := time.Parse(time.RFC3339Nano, input.FiredAt)
	if err != nil {
		return nil, fmt.Errorf("parse fired_at: %w", err)
	}

	envelope, err := definition.BuildScheduledEvent(instance, config, firedAt)
	if err != nil {
		return nil, fmt.Errorf("build scheduled event: %w", err)
	}

	task, err := a.ProcessEvent(ctx, instance, *envelope)
	if err != nil {
		return nil, fmt.Errorf("process event: %w", err)
	}

	return task, nil
}

func (a *App) ProcessEvent(ctx context.Context, instance triggerrepo.TriggerInstance, envelope EventEnvelope) (*Task, error) {
	rawConfig, err := configJSONToMap(instance.ConfigJson)
	if err != nil {
		a.emitDeliveryLog(instance, envelope, DeliveryStatusFailed, "decode trigger config", err)
		return nil, fmt.Errorf("decode trigger config: %w", err)
	}

	_, config, err := a.validateInstance(ctx, instance.ProjectID, nullUUIDToUUID(instance.EnvironmentID), instance.DefinitionSlug, rawConfig)
	if err != nil {
		a.emitDeliveryLog(instance, envelope, DeliveryStatusFailed, "validate trigger instance", err)
		return nil, fmt.Errorf("validate trigger instance: %w", err)
	}

	if instance.Status != StatusActive {
		a.emitDeliveryLog(instance, envelope, DeliveryStatusSkipped, "trigger is paused", nil)
		return nil, nil
	}

	match, err := config.Filter(envelope.Event)
	if err != nil {
		a.emitDeliveryLog(instance, envelope, DeliveryStatusFailed, "evaluate filter", err)
		return nil, fmt.Errorf("evaluate filter: %w", err)
	}
	if !match {
		a.emitDeliveryLog(instance, envelope, DeliveryStatusSkipped, "filter did not match", nil)
		return nil, nil
	}

	if err := ValidateTargetKind(instance.TargetKind); err != nil {
		a.emitDeliveryLog(instance, envelope, DeliveryStatusFailed, "trigger target is not supported", err)
		return nil, fmt.Errorf("validate trigger target kind: %w", err)
	}

	task := &Task{
		TriggerInstanceID: instance.ID.String(),
		DefinitionSlug:    instance.DefinitionSlug,
		TargetKind:        instance.TargetKind,
		TargetRef:         instance.TargetRef,
		TargetDisplay:     instance.TargetDisplay,
		EventID:           envelope.EventID,
		CorrelationID:     envelope.CorrelationID,
		EventJSON:         nil,
		RawPayload:        envelope.RawPayload,
	}
	if envelope.Event != nil {
		eventJSON, err := json.Marshal(envelope.Event)
		if err != nil {
			a.emitDeliveryLog(instance, envelope, DeliveryStatusFailed, "marshal event payload", err)
			return nil, fmt.Errorf("marshal event payload: %w", err)
		}
		task.EventJSON = eventJSON
	}

	a.emitDeliveryLog(instance, envelope, DeliveryStatusSent, "trigger event enqueued", nil)
	return task, nil
}

func (a *App) RegisterDispatcher(dispatcher Dispatcher) {
	a.dispatchers[dispatcher.Kind()] = dispatcher
}

func (a *App) Dispatch(ctx context.Context, input Task) error {
	if err := ValidateTargetKind(input.TargetKind); err != nil {
		return err
	}

	dispatcher, ok := a.dispatchers[input.TargetKind]
	if !ok {
		return fmt.Errorf("trigger dispatcher for target kind %q is not configured", input.TargetKind)
	}
	if err := dispatcher.Dispatch(ctx, input); err != nil {
		return fmt.Errorf("dispatch trigger target: %w", err)
	}
	return nil
}

func (a *App) WebhookURL(instance triggerrepo.TriggerInstance) *string {
	definition, ok := GetDefinition(instance.DefinitionSlug)
	if !ok || definition.Kind != KindWebhook {
		return nil
	}

	webhookURL := a.serverURL.JoinPath("/rpc/triggers/", instance.ID.String(), "/webhook")
	return new(webhookURL.String())
}

func (a *App) validateInstance(ctx context.Context, projectID uuid.UUID, environmentID uuid.UUID, definitionSlug string, rawConfig map[string]any) (Definition, Config, error) {
	definition, ok := GetDefinition(definitionSlug)
	if !ok {
		return Definition{}, nil, fmt.Errorf("unknown trigger definition %q", definitionSlug)
	}

	config, err := definition.DecodeConfig(rawConfig)
	if err != nil {
		return Definition{}, nil, fmt.Errorf("decode trigger config: %w", err)
	}

	hasRequiredEnv := false
	for _, requirement := range definition.EnvRequirements {
		if requirement.Required {
			hasRequiredEnv = true
			break
		}
	}

	if environmentID == uuid.Nil {
		if hasRequiredEnv {
			return Definition{}, nil, fmt.Errorf("environment_id is required when the trigger definition has required environment variables")
		}
		return definition, config, nil
	}

	envMap, err := a.loadEnvironmentMap(ctx, projectID, environmentID)
	if err != nil {
		return Definition{}, nil, fmt.Errorf("load environment: %w", err)
	}
	env := toolconfig.CIEnvFrom(envMap)
	for _, requirement := range definition.EnvRequirements {
		if requirement.Required && !env.Has(requirement.Name) {
			return Definition{}, nil, fmt.Errorf("missing required environment variable %q", requirement.Name)
		}
	}

	return definition, config, nil
}

func (a *App) reconcileSchedule(ctx context.Context, instance triggerrepo.TriggerInstance, definition Definition, config Config) error {
	if definition.Kind != KindSchedule {
		return nil
	}

	schedule, err := definition.ExtractSchedule(config)
	if err != nil {
		return fmt.Errorf("extract trigger schedule: %w", err)
	}
	if err := ScheduleTriggerCronWorkflow(ctx, a.temporalEnv, ScheduleTriggerCronWorkflowOptions{
		InstanceID:     instance.ID,
		InstanceStatus: instance.Status,
		Schedule:       schedule,
	}); err != nil {
		return fmt.Errorf("schedule trigger cron workflow: %w", err)
	}

	return nil
}

func (a *App) deleteSchedule(ctx context.Context, instance triggerrepo.TriggerInstance) error {
	definition, ok := GetDefinition(instance.DefinitionSlug)
	if !ok || definition.Kind != KindSchedule {
		return nil
	}
	if err := DeleteTriggerCronWorkflowSchedule(ctx, a.temporalEnv, instance.ID); err != nil {
		return fmt.Errorf("delete trigger cron workflow schedule: %w", err)
	}
	return nil
}

func (a *App) loadEnvironmentMap(ctx context.Context, projectID uuid.UUID, environmentID uuid.UUID) (map[string]string, error) {
	envMap, err := a.envLoader.Load(ctx, projectID, toolconfig.ID(environmentID))
	if err != nil {
		return nil, fmt.Errorf("load trigger environment entries: %w", err)
	}
	if envMap == nil {
		return map[string]string{}, nil
	}
	return envMap, nil
}

func (a *App) emitDeliveryLog(instance triggerrepo.TriggerInstance, envelope EventEnvelope, status DeliveryStatus, reason string, err error) {
	a.deliveryLogger.LogTriggerDelivery(instance, envelope, status, reason, err)
}

func ValidateTargetKind(targetKind string) error {
	switch targetKind {
	case TargetKindAssistant, TargetKindNoop:
		return nil
	default:
		return fmt.Errorf("unsupported trigger target kind %q", targetKind)
	}
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

func marshalConfigJSON(config map[string]any) ([]byte, error) {
	if config == nil {
		config = map[string]any{}
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal config json: %w", err)
	}
	return raw, nil
}

func nullUUIDToUUID(value uuid.NullUUID) uuid.UUID {
	if !value.Valid {
		return uuid.Nil
	}
	return value.UUID
}
