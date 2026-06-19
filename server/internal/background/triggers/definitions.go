package triggers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
	gjsonschema "github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/robfig/cron"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	gramjsonschema "github.com/speakeasy-api/gram/server/internal/jsonschema"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

type Config interface {
	Filter(event any) (bool, error)
}

type Kind string

const (
	KindWebhook  Kind = "webhook"
	KindSchedule Kind = "schedule"
	KindDirect   Kind = "direct"
)

type EnvRequirement struct {
	Name        string
	Description string
	Required    bool
}

type Definition struct {
	Slug                 string
	Title                string
	Description          string
	Kind                 Kind
	ConfigSchema         []byte
	CompiledConfigSchema *jsonschema.Schema
	EnvRequirements      []EnvRequirement
	EventType            reflect.Type
	DecodeConfig         func(raw map[string]any) (Config, error)
	AuthenticateWebhook  func(body []byte, headers http.Header, env map[string]string, config Config) error
	HandleWebhook        func(body []byte, headers http.Header, config Config) (*WebhookIngressResult, error)
	BuildScheduledEvent  func(instance triggerrepo.TriggerInstance, config Config, firedAt time.Time) (*EventEnvelope, error)
	BuildDirectEvent     func(instance triggerrepo.TriggerInstance, config Config, payload []byte, receivedAt time.Time) (*EventEnvelope, error)
	ExtractSchedule      func(config Config) (string, error)
}

type WebhookIngressResult struct {
	Response *WebhookResponse
	Event    *EventEnvelope
	Task     *Task
}

type WebhookResponse struct {
	Status      int
	ContentType string
	Body        []byte
}

type EventEnvelope struct {
	EventID           string
	CorrelationID     string
	TriggerInstanceID string
	DefinitionSlug    string
	Event             any
	RawPayload        []byte
	ReceivedAt        time.Time
}

type Task struct {
	TriggerInstanceID string
	DefinitionSlug    string
	TargetKind        string
	TargetRef         string
	TargetDisplay     string
	EventID           string
	CorrelationID     string
	EventJSON         []byte
	RawPayload        []byte
}

type cronTriggerConfig struct {
	Schedule string  `json:"schedule"`
	Note     *string `json:"note,omitempty"`
}

func (c cronTriggerConfig) Filter(_ any) (bool, error) { return true, nil }

// MaxWakeHorizon caps how far in the future a wake trigger may be scheduled.
const MaxWakeHorizon = 30 * 24 * time.Hour

// ValidateWakeFireAt enforces the create-time bounds on a wake's fire_at.
// Pure helper so callers can test bounds without round-tripping a DB.
func ValidateWakeFireAt(fireAt, now time.Time) error {
	if fireAt.IsZero() {
		return fmt.Errorf("fire_at is required")
	}
	if !fireAt.After(now) {
		return fmt.Errorf("fire_at must be in the future")
	}
	if fireAt.After(now.Add(MaxWakeHorizon)) {
		return fmt.Errorf("fire_at must be within %s", MaxWakeHorizon)
	}
	return nil
}

// WakeConfigFields decodes a wake trigger instance's config_json blob and
// returns the (correlation_id, fire_at) pair used by audit log call sites.
// Returns empty strings on decode failure so callers can still record audit
// events without taking down the workflow.
func WakeConfigFields(configJSON []byte) (string, string) {
	var cfg struct {
		CorrelationID string `json:"correlation_id"`
		FireAt        string `json:"fire_at"`
	}
	if len(configJSON) == 0 {
		return "", ""
	}
	_ = json.Unmarshal(configJSON, &cfg)
	return cfg.CorrelationID, cfg.FireAt
}

type wakeTriggerConfig struct {
	FireAt        time.Time `json:"fire_at"`
	Note          *string   `json:"note,omitempty"`
	CorrelationID string    `json:"correlation_id"`
}

func (c wakeTriggerConfig) Filter(_ any) (bool, error) { return true, nil }

type dashboardTriggerConfig struct{}

func (dashboardTriggerConfig) Filter(_ any) (bool, error) { return true, nil }

type dashboardTriggerEvent struct {
	Text           string `json:"text" cel:"text"`
	UserID         string `json:"user_id,omitempty" cel:"user_id"`
	CorrelationID  string `json:"correlation_id,omitempty" cel:"correlation_id"`
	IdempotencyKey string `json:"idempotency_key,omitempty" cel:"idempotency_key"`
}

type cronTriggerEvent struct {
	Schedule          string `json:"schedule" cel:"schedule"`
	FiredAt           string `json:"fired_at" cel:"fired_at"`
	TriggerInstanceID string `json:"trigger_instance_id" cel:"trigger_instance_id"`
	Note              string `json:"note,omitempty" cel:"note"`
}

type wakeTriggerEvent struct {
	FiredAt           string `json:"fired_at" cel:"fired_at"`
	ScheduledAt       string `json:"scheduled_at" cel:"scheduled_at"`
	TriggerInstanceID string `json:"trigger_instance_id" cel:"trigger_instance_id"`
	Note              string `json:"note,omitempty" cel:"note"`
}

var registry = map[string]Definition{
	DefinitionSlugSlack:     newSlackDefinition(),
	DefinitionSlugLinear:    newLinearDefinition(),
	DefinitionSlugGithub:    newGitHubDefinition(),
	DefinitionSlugCron:      newCronDefinition(),
	DefinitionSlugWake:      newWakeDefinition(),
	DefinitionSlugDashboard: newDashboardDefinition(),
}

func List() []Definition {
	definitions := make([]Definition, 0, len(registry))
	for _, definition := range registry {
		// Direct-ingress definitions (e.g. dashboard) are system-managed, not
		// user-creatable trigger types — keep them out of the public catalog.
		if definition.Kind == KindDirect {
			continue
		}
		definitions = append(definitions, definition)
	}
	slices.SortFunc(definitions, func(a, b Definition) int {
		return strings.Compare(a.Slug, b.Slug)
	})
	return definitions
}

func GetDefinition(slug string) (Definition, bool) {
	definition, ok := registry[slug]
	return definition, ok
}

func compileCELFilter(eventType reflect.Type, expression string) (cel.Program, error) {
	if strings.TrimSpace(expression) == "" {
		return nil, nil
	}

	env, err := newCELEnv(eventType)
	if err != nil {
		return nil, fmt.Errorf("create CEL env: %w", err)
	}

	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile filter: %w", issues.Err())
	}
	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("filter must evaluate to bool")
	}

	prog, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("build filter program: %w", err)
	}

	return prog, nil
}

func newCronDefinition() Definition {
	schema := buildInputSchema[cronTriggerConfig]()
	compiled := mustCompileSchema(schema)
	return Definition{
		Slug:                 DefinitionSlugCron,
		Title:                "Cron",
		Description:          "Run a trigger on a Temporal-backed cron schedule.",
		Kind:                 KindSchedule,
		ConfigSchema:         schema,
		CompiledConfigSchema: compiled,
		EnvRequirements:      []EnvRequirement{},
		EventType:            reflect.TypeFor[cronTriggerEvent](),
		DecodeConfig: func(raw map[string]any) (Config, error) {
			cfg, err := decodeConfig[cronTriggerConfig](raw, compiled)
			if err != nil {
				return nil, err
			}
			if _, err := cron.ParseStandard(cfg.Schedule); err != nil {
				return nil, fmt.Errorf("parse schedule: %w", err)
			}
			return cfg, nil
		},
		AuthenticateWebhook: nil,
		HandleWebhook:       nil,
		BuildScheduledEvent: func(instance triggerrepo.TriggerInstance, config Config, firedAt time.Time) (*EventEnvelope, error) {
			cfg, ok := config.(cronTriggerConfig)
			if !ok {
				return nil, fmt.Errorf("invalid cron config")
			}
			note := ""
			if cfg.Note != nil {
				note = *cfg.Note
			}
			event := cronTriggerEvent{
				Schedule:          cfg.Schedule,
				FiredAt:           firedAt.UTC().Format(time.RFC3339Nano),
				TriggerInstanceID: instance.ID.String(),
				Note:              note,
			}
			rawPayload, err := json.Marshal(event)
			if err != nil {
				return nil, fmt.Errorf("marshal cron event: %w", err)
			}
			return &EventEnvelope{
				EventID:           uuid.NewSHA1(uuid.NameSpaceURL, []byte(instance.ID.String()+":"+event.FiredAt)).String(),
				CorrelationID:     instance.ID.String(),
				TriggerInstanceID: instance.ID.String(),
				DefinitionSlug:    instance.DefinitionSlug,
				Event:             event,
				RawPayload:        rawPayload,
				ReceivedAt:        firedAt.UTC(),
			}, nil
		},
		BuildDirectEvent: nil,
		ExtractSchedule: func(config Config) (string, error) {
			cfg, ok := config.(cronTriggerConfig)
			if !ok {
				return "", fmt.Errorf("invalid cron config")
			}
			return cfg.Schedule, nil
		},
	}
}

func newWakeDefinition() Definition {
	schema := buildInputSchema[wakeTriggerConfig]()
	compiled := mustCompileSchema(schema)
	return Definition{
		Slug:                 DefinitionSlugWake,
		Title:                "Wake",
		Description:          "One-shot self-wake of an assistant thread at an absolute future time.",
		Kind:                 KindSchedule,
		ConfigSchema:         schema,
		CompiledConfigSchema: compiled,
		EnvRequirements:      []EnvRequirement{},
		EventType:            reflect.TypeFor[wakeTriggerEvent](),
		DecodeConfig: func(raw map[string]any) (Config, error) {
			cfg, err := decodeConfig[wakeTriggerConfig](raw, compiled)
			if err != nil {
				return nil, err
			}
			if cfg.FireAt.IsZero() {
				return nil, fmt.Errorf("fire_at is required")
			}
			if strings.TrimSpace(cfg.CorrelationID) == "" {
				return nil, fmt.Errorf("correlation_id is required")
			}
			return cfg, nil
		},
		AuthenticateWebhook: nil,
		HandleWebhook:       nil,
		BuildScheduledEvent: func(instance triggerrepo.TriggerInstance, config Config, firedAt time.Time) (*EventEnvelope, error) {
			cfg, ok := config.(wakeTriggerConfig)
			if !ok {
				return nil, fmt.Errorf("invalid wake config")
			}
			note := ""
			if cfg.Note != nil {
				note = *cfg.Note
			}
			event := wakeTriggerEvent{
				FiredAt:           firedAt.UTC().Format(time.RFC3339Nano),
				ScheduledAt:       cfg.FireAt.UTC().Format(time.RFC3339Nano),
				TriggerInstanceID: instance.ID.String(),
				Note:              note,
			}
			rawPayload, err := json.Marshal(event)
			if err != nil {
				return nil, fmt.Errorf("marshal wake event: %w", err)
			}
			return &EventEnvelope{
				EventID:           uuid.NewSHA1(uuid.NameSpaceURL, []byte(instance.ID.String()+":wake:"+event.ScheduledAt)).String(),
				CorrelationID:     cfg.CorrelationID,
				TriggerInstanceID: instance.ID.String(),
				DefinitionSlug:    instance.DefinitionSlug,
				Event:             event,
				RawPayload:        rawPayload,
				ReceivedAt:        firedAt.UTC(),
			}, nil
		},
		BuildDirectEvent: nil,
		ExtractSchedule: func(config Config) (string, error) {
			cfg, ok := config.(wakeTriggerConfig)
			if !ok {
				return "", fmt.Errorf("invalid wake config")
			}
			return cfg.FireAt.UTC().Format(time.RFC3339Nano), nil
		},
	}
}

func newDashboardDefinition() Definition {
	schema := buildInputSchema[dashboardTriggerConfig]()
	compiled := mustCompileSchema(schema)
	return Definition{
		Slug:                 DefinitionSlugDashboard,
		Title:                "Dashboard",
		Description:          "Direct messages from the Gram dashboard assistant sidebar.",
		Kind:                 KindDirect,
		ConfigSchema:         schema,
		CompiledConfigSchema: compiled,
		EnvRequirements:      []EnvRequirement{},
		EventType:            reflect.TypeFor[dashboardTriggerEvent](),
		DecodeConfig: func(raw map[string]any) (Config, error) {
			return decodeConfig[dashboardTriggerConfig](raw, compiled)
		},
		AuthenticateWebhook: nil,
		HandleWebhook:       nil,
		BuildScheduledEvent: nil,
		BuildDirectEvent: func(instance triggerrepo.TriggerInstance, _ Config, payload []byte, receivedAt time.Time) (*EventEnvelope, error) {
			var event dashboardTriggerEvent
			if err := json.Unmarshal(payload, &event); err != nil {
				return nil, fmt.Errorf("decode dashboard message: %w", err)
			}
			if event.Text == "" {
				return nil, fmt.Errorf("dashboard message text is required")
			}
			if event.UserID == "" {
				return nil, fmt.Errorf("dashboard message user id is required")
			}
			if event.IdempotencyKey == "" {
				return nil, fmt.Errorf("dashboard message idempotency key is required")
			}
			if event.CorrelationID == "" {
				return nil, fmt.Errorf("dashboard message correlation id is required")
			}
			return &EventEnvelope{
				EventID:           uuid.NewSHA1(uuid.NameSpaceURL, []byte(instance.ID.String()+":"+event.IdempotencyKey)).String(),
				CorrelationID:     event.CorrelationID,
				TriggerInstanceID: instance.ID.String(),
				DefinitionSlug:    instance.DefinitionSlug,
				Event:             event,
				RawPayload:        payload,
				ReceivedAt:        receivedAt.UTC(),
			}, nil
		},
		ExtractSchedule: nil,
	}
}

type inputSchemaConfig struct {
	forOptions       *gjsonschema.ForOptions
	propertyMutators map[string][]func(*gjsonschema.Schema)
}

type inputSchemaOption func(*inputSchemaConfig)

func buildInputSchema[T any](options ...inputSchemaOption) []byte {
	config := &inputSchemaConfig{
		forOptions: &gjsonschema.ForOptions{
			IgnoreInvalidTypes: false,
			TypeSchemas:        map[reflect.Type]*gjsonschema.Schema{},
		},
		propertyMutators: map[string][]func(*gjsonschema.Schema){},
	}

	for _, option := range options {
		option(config)
	}

	schema, err := gjsonschema.For[T](config.forOptions)
	if err != nil {
		panic(fmt.Errorf("build input schema: %w", err))
	}

	for propertyName, mutators := range config.propertyMutators {
		prop := schema.Properties[propertyName]
		if prop == nil {
			continue
		}
		for _, mutate := range mutators {
			mutate(prop)
		}
	}

	bs, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Errorf("marshal schema: %w", err))
	}
	return bs
}

func withArrayItemsEnum(propertyName string, values ...any) inputSchemaOption {
	return func(config *inputSchemaConfig) {
		config.propertyMutators[propertyName] = append(config.propertyMutators[propertyName], func(prop *gjsonschema.Schema) {
			if prop.Items == nil {
				prop.Items = new(gjsonschema.Schema)
			}
			prop.Items.Enum = values
		})
	}
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func newCELEnv(eventType reflect.Type) (*cel.Env, error) {
	if eventType.Kind() == reflect.Pointer {
		eventType = eventType.Elem()
	}

	env, err := cel.NewEnv(
		ext.NativeTypes(eventType, ext.ParseStructTags(true)),
		cel.Variable("event", cel.ObjectType(celTypeName(eventType))),
	)
	if err != nil {
		return nil, fmt.Errorf("create CEL env: %w", err)
	}
	return env, nil
}

func celTypeName(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return path.Base(t.PkgPath()) + "." + t.Name()
}

func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func mustCompileSchema(schema []byte) *jsonschema.Schema {
	compiled, err := gramjsonschema.CompileSchema(schema)
	if err != nil {
		panic(fmt.Errorf("compile trigger schema: %w", err))
	}
	return compiled
}

func decodeConfig[T Config](raw map[string]any, schema *jsonschema.Schema) (T, error) {
	var zero T
	if raw == nil {
		raw = map[string]any{}
	}
	if err := gramjsonschema.ValidateAgainstSchema(schema, raw); err != nil {
		return zero, fmt.Errorf("validate config: %w", err)
	}
	bs, err := json.Marshal(raw)
	if err != nil {
		return zero, fmt.Errorf("marshal config: %w", err)
	}
	var cfg T
	if err := json.Unmarshal(bs, &cfg); err != nil {
		return zero, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}
