package triggers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	sourceTriggers         = "triggers"
	toolNameListTriggers   = "platform_list_triggers"
	toolNameConfigure      = "platform_configure_trigger"
	targetKindAssistant    = bgtriggers.TargetKindAssistant
	targetKindNoop         = bgtriggers.TargetKindNoop
	triggerStatusActive    = bgtriggers.StatusActive
	triggerStatusPaused    = bgtriggers.StatusPaused
	triggerStatusCancelled = bgtriggers.StatusCancelled
)

type listTriggersInput struct {
	DefinitionSlug *string `json:"definition_slug,omitempty" jsonschema:"Optional trigger definition filter."`
}

type configureTriggerSharedInput struct {
	TriggerID       *string `json:"trigger_id,omitempty" jsonschema:"Existing trigger instance ID to update. Leave empty to create a new trigger."`
	DefinitionSlug  string  `json:"definition_slug" jsonschema:"Trigger definition slug."`
	Name            string  `json:"name" jsonschema:"Trigger instance name."`
	EnvironmentSlug string  `json:"environment_slug" jsonschema:"Environment slug containing the trigger secrets."`
	Status          *string `json:"status,omitempty" jsonschema:"Optional trigger status."`
	TargetKind      string  `json:"target_kind" jsonschema:"Trigger target kind."`
	TargetRef       string  `json:"target_ref" jsonschema:"Opaque target reference to invoke when this trigger matches."`
	TargetDisplay   string  `json:"target_display" jsonschema:"User-facing target label."`
}

type configureTriggerInput struct {
	TriggerID       *string        `json:"trigger_id,omitempty"`
	DefinitionSlug  string         `json:"definition_slug"`
	Name            string         `json:"name"`
	EnvironmentSlug string         `json:"environment_slug"`
	Status          *string        `json:"status,omitempty"`
	TargetKind      string         `json:"target_kind"`
	TargetRef       string         `json:"target_ref"`
	TargetDisplay   string         `json:"target_display"`
	Config          map[string]any `json:"config"`
}

// Anthropic's tool validator rejects oneOf/anyOf/allOf at the top level of
// input_schema. The envelope sinks the discriminated union one layer deep so
// definition_slug and config can still co-vary inside oneOf.
type configureTriggerInputEnvelope struct {
	Input configureTriggerInput `json:"input"`
}

type triggerToolView struct {
	ID              string         `json:"id"`
	DefinitionSlug  string         `json:"definition_slug"`
	Name            string         `json:"name"`
	EnvironmentID   string         `json:"environment_id"`
	EnvironmentSlug string         `json:"environment_slug"`
	TargetKind      string         `json:"target_kind"`
	TargetRef       string         `json:"target_ref"`
	TargetDisplay   string         `json:"target_display"`
	Status          string         `json:"status"`
	Config          map[string]any `json:"config"`
	WebhookURL      *string        `json:"webhook_url,omitempty"`
	CreatedAt       string         `json:"created_at"`
	UpdatedAt       string         `json:"updated_at"`
}

type configureTriggerResult struct {
	Action  string          `json:"action"`
	Trigger triggerToolView `json:"trigger"`
}

type listTriggersResult struct {
	Triggers []triggerToolView `json:"triggers"`
}

type ListTriggers struct {
	db                  *pgxpool.Pool
	app                 *bgtriggers.App
	assistantSelfScoped bool
}

type ConfigureTrigger struct {
	db                  *pgxpool.Pool
	app                 *bgtriggers.App
	inputSchema         []byte
	audit               *audit.Logger
	assistantSelfScoped bool
}

func NewListTriggersTool(db *pgxpool.Pool, app *bgtriggers.App) *ListTriggers {
	return &ListTriggers{db: db, app: app, assistantSelfScoped: false}
}

// NewAssistantListTriggersTool returns a ListTriggers that filters the result
// to triggers whose target is the calling assistant principal.
func NewAssistantListTriggersTool(db *pgxpool.Pool, app *bgtriggers.App) *ListTriggers {
	return &ListTriggers{db: db, app: app, assistantSelfScoped: true}
}

func NewConfigureTriggerTool(db *pgxpool.Pool, app *bgtriggers.App, audit *audit.Logger) *ConfigureTrigger {
	return &ConfigureTrigger{
		db:                  db,
		app:                 app,
		inputSchema:         buildConfigureTriggerInputSchema(false),
		audit:               audit,
		assistantSelfScoped: false,
	}
}

// NewAssistantConfigureTriggerTool returns a ConfigureTrigger that pins
// target_kind/target_ref to the calling assistant principal and strips them
// from the visible schema, so the LLM cannot redirect a trigger at a sibling
// assistant in the same project.
func NewAssistantConfigureTriggerTool(db *pgxpool.Pool, app *bgtriggers.App, audit *audit.Logger) *ConfigureTrigger {
	return &ConfigureTrigger{
		db:                  db,
		app:                 app,
		inputSchema:         buildConfigureTriggerInputSchema(true),
		audit:               audit,
		assistantSelfScoped: true,
	}
}

func buildConfigureTriggerInputSchema(assistantSelfScoped bool) []byte {
	definitionSlugs := listDefinitionSlugs()
	inner := schemaBytesToMap(core.BuildInputSchema[configureTriggerSharedInput](
		core.WithPropertyFormat("trigger_id", "uuid"),
		core.WithPropertyEnum("definition_slug", stringSliceToAny(definitionSlugs)...),
		core.WithPropertyEnum("status", triggerStatusActive, triggerStatusPaused, triggerStatusCancelled),
		core.WithPropertyEnum("target_kind", targetKindAssistant, targetKindNoop),
	))

	properties := getMap(inner, "properties")
	properties["config"] = map[string]any{
		"type":        "object",
		"description": "Trigger-definition-specific configuration.",
	}

	required := append(getStringSlice(inner, "required"), "config")
	if assistantSelfScoped {
		// Target binds to the calling assistant principal; do not let the LLM
		// see, supply, or override it.
		stripSchemaProperty(inner, "target_kind")
		stripSchemaProperty(inner, "target_ref")
		stripSchemaProperty(inner, "target_display")
		filtered := required[:0]
		for _, name := range required {
			switch name {
			case "target_kind", "target_ref", "target_display":
				continue
			}
			filtered = append(filtered, name)
		}
		required = filtered
	}
	inner["required"] = dedupeStrings(required)

	branches := make([]any, 0, len(definitionSlugs))
	for _, slug := range definitionSlugs {
		definition, ok := bgtriggers.GetDefinition(slug)
		if !ok {
			panic(fmt.Errorf("missing trigger definition %q", slug))
		}
		configSchema := schemaBytesToMap(definition.ConfigSchema)
		if slug == bgtriggers.DefinitionSlugWake {
			// correlation_id is injected by the platform tool from the calling
			// assistant principal; the LLM must not be allowed to forge one
			// pointing at another thread.
			stripSchemaProperty(configSchema, "correlation_id")
		}
		branches = append(branches, map[string]any{
			"properties": map[string]any{
				"definition_slug": map[string]any{
					"const": slug,
				},
				"config": configSchema,
			},
			"required": []string{"definition_slug", "config"},
		})
	}
	inner["oneOf"] = branches

	return mustMarshalSchemaMap(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": inner,
		},
		"required":             []any{"input"},
		"additionalProperties": false,
	})
}

func stripSchemaProperty(schema map[string]any, name string) {
	if props, ok := schema["properties"].(map[string]any); ok {
		delete(props, name)
	}
	if required, ok := schema["required"].([]any); ok {
		filtered := required[:0]
		for _, item := range required {
			if s, ok := item.(string); ok && s == name {
				continue
			}
			filtered = append(filtered, item)
		}
		schema["required"] = filtered
	}
}

func listDefinitionSlugs() []string {
	definitions := bgtriggers.List()
	slugs := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		slugs = append(slugs, definition.Slug)
	}
	sort.Strings(slugs)
	return slugs
}

func schemaBytesToMap(schema []byte) map[string]any {
	var out map[string]any
	if err := json.Unmarshal(schema, &out); err != nil {
		panic(fmt.Errorf("decode schema bytes: %w", err))
	}
	if out == nil {
		return map[string]any{}
	}
	return out
}

func mustMarshalSchemaMap(schema map[string]any) []byte {
	bs, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Errorf("marshal schema map: %w", err))
	}
	return bs
}

func getMap(parent map[string]any, key string) map[string]any {
	value, ok := parent[key].(map[string]any)
	if ok && value != nil {
		return value
	}
	value = map[string]any{}
	parent[key] = value
	return value
}

func getStringSlice(parent map[string]any, key string) []string {
	raw, ok := parent[key].([]any)
	if !ok {
		if direct, ok := parent[key].([]string); ok {
			return append([]string(nil), direct...)
		}
		return nil
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		value, ok := item.(string)
		if ok {
			values = append(values, value)
		}
	}
	return values
}

func stringSliceToAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func (t *ListTriggers) Descriptor() core.ToolDescriptor {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := false

	return core.ToolDescriptor{
		SourceSlug:  sourceTriggers,
		HandlerName: "list_triggers",
		Name:        toolNameListTriggers,
		Description: "List trigger instances configured for the current project.",
		InputSchema: core.BuildInputSchema[listTriggersInput](
			core.WithPropertyEnum("definition_slug", stringSliceToAny(listDefinitionSlugs())...),
		),
		Annotations: triggerToolAnnotations(readOnly, destructive, idempotent, openWorld),
		Managed:     true,
		Variables:   nil,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *ListTriggers) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.db == nil || t.app == nil {
		return fmt.Errorf("trigger tools are not configured")
	}

	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return err
	}

	var selfTargetRef string
	if t.assistantSelfScoped {
		principal, ok := contextvalues.GetAssistantPrincipal(ctx)
		if !ok {
			return fmt.Errorf("assistant list-triggers requires an assistant principal")
		}
		selfTargetRef = principal.AssistantID.String()
	}

	input := listTriggersInput{DefinitionSlug: nil}
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	envQueries := environmentsrepo.New(t.db)

	items, err := t.app.ListInstances(ctx, *authCtx.ProjectID)
	if err != nil {
		return fmt.Errorf("list trigger instances: %w", err)
	}

	result := listTriggersResult{Triggers: make([]triggerToolView, 0, len(items))}
	for _, item := range items {
		if input.DefinitionSlug != nil && *input.DefinitionSlug != "" && item.DefinitionSlug != *input.DefinitionSlug {
			continue
		}
		if t.assistantSelfScoped && (item.TargetKind != targetKindAssistant || item.TargetRef != selfTargetRef) {
			continue
		}

		view, err := buildTriggerToolView(ctx, envQueries, *authCtx.ProjectID, item, t.app)
		if err != nil {
			return err
		}
		result.Triggers = append(result.Triggers, view)
	}

	if err := json.NewEncoder(wr).Encode(result); err != nil {
		return fmt.Errorf("encode list trigger result: %w", err)
	}
	return nil
}

func (t *ConfigureTrigger) Descriptor() core.ToolDescriptor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := false

	return core.ToolDescriptor{
		SourceSlug:  sourceTriggers,
		HandlerName: "configure_trigger",
		Name:        toolNameConfigure,
		Description: "Create or update a trigger instance using a definition-specific config schema selected by definition_slug.",
		InputSchema: t.inputSchema,
		Annotations: triggerToolAnnotations(readOnly, destructive, idempotent, openWorld),
		Managed:     true,
		Variables:   nil,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *ConfigureTrigger) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.db == nil || t.app == nil {
		return fmt.Errorf("trigger tools are not configured")
	}

	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return err
	}

	var envelope configureTriggerInputEnvelope
	if err := decodePayload(payload, &envelope); err != nil {
		return err
	}

	result, err := t.upsertTrigger(ctx, authCtx, configureTriggerParams(envelope.Input))
	if err != nil {
		return err
	}

	if err := json.NewEncoder(wr).Encode(result); err != nil {
		return fmt.Errorf("encode configure trigger result: %w", err)
	}
	return nil
}

type configureTriggerParams struct {
	TriggerID       *string
	DefinitionSlug  string
	Name            string
	EnvironmentSlug string
	Status          *string
	TargetKind      string
	TargetRef       string
	TargetDisplay   string
	Config          map[string]any
}

func (t *ConfigureTrigger) upsertTrigger(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	params configureTriggerParams,
) (*configureTriggerResult, error) {
	if strings.TrimSpace(params.DefinitionSlug) == "" {
		return nil, fmt.Errorf("definition_slug is required")
	}
	if _, ok := bgtriggers.GetDefinition(params.DefinitionSlug); !ok {
		return nil, fmt.Errorf("unsupported trigger definition %q", params.DefinitionSlug)
	}
	if params.DefinitionSlug == bgtriggers.DefinitionSlugWake {
		return t.upsertWake(ctx, authCtx, params)
	}
	if t.assistantSelfScoped {
		principal, ok := contextvalues.GetAssistantPrincipal(ctx)
		if !ok {
			return nil, fmt.Errorf("assistant configure-trigger requires an assistant principal")
		}
		params.TargetKind = targetKindAssistant
		params.TargetRef = principal.AssistantID.String()
		if strings.TrimSpace(params.TargetDisplay) == "" {
			params.TargetDisplay = strings.TrimSpace(params.Name)
		}
	}
	if strings.TrimSpace(params.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}
	if strings.TrimSpace(params.EnvironmentSlug) == "" {
		return nil, fmt.Errorf("environment_slug is required")
	}
	if strings.TrimSpace(params.TargetRef) == "" {
		return nil, fmt.Errorf("target_ref is required")
	}
	if strings.TrimSpace(params.TargetDisplay) == "" {
		return nil, fmt.Errorf("target_display is required")
	}

	targetKind, err := normalizeTargetKind(params.TargetKind)
	if err != nil {
		return nil, err
	}

	envQueries := environmentsrepo.New(t.db)

	environment, err := envQueries.GetEnvironmentBySlug(ctx, environmentsrepo.GetEnvironmentBySlugParams{
		ProjectID: *authCtx.ProjectID,
		Slug:      strings.ToLower(strings.TrimSpace(params.EnvironmentSlug)),
	})
	if err != nil {
		return nil, fmt.Errorf("get environment by slug: %w", err)
	}

	if params.Config == nil {
		params.Config = map[string]any{}
	}

	status := normalizeStatus(params.Status)
	action := "created"

	actorPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)

	var item triggerrepo.TriggerInstance
	if params.TriggerID == nil || strings.TrimSpace(*params.TriggerID) == "" {
		item, err = t.app.Create(ctx, bgtriggers.CreateParams{
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,
			DefinitionSlug: params.DefinitionSlug,
			Name:           strings.TrimSpace(params.Name),
			EnvironmentID:  uuid.NullUUID{UUID: environment.ID, Valid: true},
			TargetKind:     targetKind,
			TargetRef:      strings.TrimSpace(params.TargetRef),
			TargetDisplay:  strings.TrimSpace(params.TargetDisplay),
			Config:         params.Config,
			Status:         status,
		}, func(ctx context.Context, dbtx pgx.Tx, instance triggerrepo.TriggerInstance) error {
			return t.audit.LogTriggerInstanceCreate(ctx, dbtx, audit.LogTriggerInstanceCreateEvent{
				OrganizationID:     authCtx.ActiveOrganizationID,
				ProjectID:          *authCtx.ProjectID,
				Actor:              actorPrincipal,
				ActorDisplayName:   authCtx.Email,
				ActorSlug:          nil,
				TriggerInstanceURN: urn.NewTriggerInstance(instance.ID),
				Name:               instance.Name,
				DefinitionSlug:     instance.DefinitionSlug,
			})
		})
		if err != nil {
			return nil, fmt.Errorf("create trigger instance: %w", err)
		}
	} else {
		action = "updated"
		triggerID, err := uuid.Parse(strings.TrimSpace(*params.TriggerID))
		if err != nil {
			return nil, fmt.Errorf("parse trigger_id: %w", err)
		}

		existing, err := t.app.GetInstance(ctx, *authCtx.ProjectID, triggerID)
		if err != nil {
			return nil, fmt.Errorf("get trigger instance: %w", err)
		}
		if existing.DefinitionSlug != params.DefinitionSlug {
			return nil, fmt.Errorf("trigger %s is %q, expected %q", existing.ID.String(), existing.DefinitionSlug, params.DefinitionSlug)
		}
		if t.assistantSelfScoped && (existing.TargetKind != targetKindAssistant || existing.TargetRef != params.TargetRef) {
			return nil, fmt.Errorf("trigger does not belong to the calling assistant")
		}

		beforeView, err := buildTriggerInstanceSnapshot(existing, t.app.WebhookURL(existing))
		if err != nil {
			return nil, fmt.Errorf("build trigger instance before-snapshot: %w", err)
		}

		item, err = t.app.Update(ctx, bgtriggers.UpdateParams{
			ID:             triggerID,
			ProjectID:      *authCtx.ProjectID,
			DefinitionSlug: params.DefinitionSlug,
			Name:           strings.TrimSpace(params.Name),
			EnvironmentID:  uuid.NullUUID{UUID: environment.ID, Valid: true},
			TargetKind:     targetKind,
			TargetRef:      strings.TrimSpace(params.TargetRef),
			TargetDisplay:  strings.TrimSpace(params.TargetDisplay),
			Config:         params.Config,
			Status:         status,
		}, func(ctx context.Context, dbtx pgx.Tx, instance triggerrepo.TriggerInstance) error {
			afterView, err := buildTriggerInstanceSnapshot(instance, t.app.WebhookURL(instance))
			if err != nil {
				return fmt.Errorf("build trigger instance after-snapshot: %w", err)
			}
			return t.audit.LogTriggerInstanceUpdate(ctx, dbtx, audit.LogTriggerInstanceUpdateEvent{
				OrganizationID:                authCtx.ActiveOrganizationID,
				ProjectID:                     *authCtx.ProjectID,
				Actor:                         actorPrincipal,
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
			return nil, fmt.Errorf("update trigger instance: %w", err)
		}
	}

	view, err := buildTriggerToolView(ctx, envQueries, *authCtx.ProjectID, item, t.app)
	if err != nil {
		return nil, err
	}

	return &configureTriggerResult{
		Action:  action,
		Trigger: view,
	}, nil
}

// upsertWake handles the wake-specific create/cancel path. Wake triggers are
// always assistant-scoped — target_kind, target_ref, and correlation_id come
// from the calling assistant principal, not the LLM. environment_slug is
// optional (wake has no env requirements). The only update operation
// supported is cancellation (status='cancelled').
func (t *ConfigureTrigger) upsertWake(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	params configureTriggerParams,
) (*configureTriggerResult, error) {
	principal, ok := contextvalues.GetAssistantPrincipal(ctx)
	if !ok {
		return nil, fmt.Errorf("wake triggers require an assistant principal")
	}

	envQueries := environmentsrepo.New(t.db)
	actorPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)

	if params.TriggerID != nil && strings.TrimSpace(*params.TriggerID) != "" {
		return t.cancelWake(ctx, authCtx, envQueries, actorPrincipal, principal, params)
	}

	if strings.TrimSpace(params.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}

	thread, err := assistantrepo.New(t.db).ResolveThreadCorrelation(ctx, principal.ThreadID)
	if err != nil {
		return nil, fmt.Errorf("resolve thread correlation: %w", err)
	}
	if thread.ProjectID != *authCtx.ProjectID || thread.AssistantID != principal.AssistantID {
		return nil, fmt.Errorf("thread does not belong to the calling assistant")
	}

	fireAt, note, err := decodeWakeInput(params.Config)
	if err != nil {
		return nil, err
	}

	targetDisplay := strings.TrimSpace(params.TargetDisplay)
	if targetDisplay == "" {
		targetDisplay = strings.TrimSpace(params.Name)
	}

	instance, err := t.app.CreateWakeInstance(ctx, bgtriggers.CreateWakeInstanceParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           strings.TrimSpace(params.Name),
		AssistantID:    principal.AssistantID,
		TargetDisplay:  targetDisplay,
		FireAt:         fireAt.UTC(),
		Note:           note,
		CorrelationID:  thread.CorrelationID,
	}, func(ctx context.Context, dbtx pgx.Tx, item triggerrepo.TriggerInstance) error {
		return t.audit.LogWakeScheduled(ctx, dbtx, audit.LogWakeEvent{
			OrganizationID:     authCtx.ActiveOrganizationID,
			ProjectID:          *authCtx.ProjectID,
			Actor:              actorPrincipal,
			ActorDisplayName:   authCtx.Email,
			ActorSlug:          nil,
			TriggerInstanceURN: urn.NewTriggerInstance(item.ID),
			Name:               item.Name,
			Correlation:        thread.CorrelationID,
			FireAt:             fireAt.UTC().Format(time.RFC3339Nano),
		})
	})
	if err != nil {
		return nil, fmt.Errorf("create wake instance: %w", err)
	}

	view, err := buildTriggerToolView(ctx, envQueries, *authCtx.ProjectID, instance, t.app)
	if err != nil {
		return nil, err
	}
	return &configureTriggerResult{Action: "created", Trigger: view}, nil
}

func (t *ConfigureTrigger) cancelWake(
	ctx context.Context,
	authCtx *contextvalues.AuthContext,
	envQueries *environmentsrepo.Queries,
	actorPrincipal urn.Principal,
	principal contextvalues.AssistantPrincipal,
	params configureTriggerParams,
) (*configureTriggerResult, error) {
	if params.Status == nil || strings.TrimSpace(*params.Status) != triggerStatusCancelled {
		return nil, fmt.Errorf("wake triggers only support status=cancelled on update")
	}
	triggerID, err := uuid.Parse(strings.TrimSpace(*params.TriggerID))
	if err != nil {
		return nil, fmt.Errorf("parse trigger_id: %w", err)
	}

	existing, err := t.app.GetInstance(ctx, *authCtx.ProjectID, triggerID)
	if err != nil {
		return nil, fmt.Errorf("get wake instance: %w", err)
	}
	if existing.DefinitionSlug != bgtriggers.DefinitionSlugWake {
		return nil, fmt.Errorf("trigger is not a wake")
	}
	if existing.TargetKind != bgtriggers.TargetKindAssistant || existing.TargetRef != principal.AssistantID.String() {
		return nil, fmt.Errorf("wake does not belong to the calling assistant")
	}

	correlationID, fireAt := bgtriggers.WakeConfigFields(existing.ConfigJson)

	item, err := t.app.CancelWakeInstance(ctx, *authCtx.ProjectID, triggerID, func(ctx context.Context, dbtx pgx.Tx, instance triggerrepo.TriggerInstance) error {
		return t.audit.LogWakeCancelled(ctx, dbtx, audit.LogWakeEvent{
			OrganizationID:     authCtx.ActiveOrganizationID,
			ProjectID:          *authCtx.ProjectID,
			Actor:              actorPrincipal,
			ActorDisplayName:   authCtx.Email,
			ActorSlug:          nil,
			TriggerInstanceURN: urn.NewTriggerInstance(instance.ID),
			Name:               instance.Name,
			Correlation:        correlationID,
			FireAt:             fireAt,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("cancel wake instance: %w", err)
	}

	view, err := buildTriggerToolView(ctx, envQueries, *authCtx.ProjectID, item, t.app)
	if err != nil {
		return nil, err
	}
	return &configureTriggerResult{Action: "updated", Trigger: view}, nil
}

func decodeWakeInput(config map[string]any) (time.Time, *string, error) {
	rawFireAt, ok := config["fire_at"].(string)
	if !ok || strings.TrimSpace(rawFireAt) == "" {
		return time.Time{}, nil, fmt.Errorf("config.fire_at is required")
	}
	fireAt, err := time.Parse(time.RFC3339, strings.TrimSpace(rawFireAt))
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("parse fire_at: %w", err)
	}
	var note *string
	if raw, ok := config["note"]; ok && raw != nil {
		if s, ok := raw.(string); ok && s != "" {
			note = &s
		}
	}
	return fireAt, note, nil
}

func buildTriggerToolView(
	ctx context.Context,
	envQueries *environmentsrepo.Queries,
	projectID uuid.UUID,
	item triggerrepo.TriggerInstance,
	app *bgtriggers.App,
) (triggerToolView, error) {
	config := map[string]any{}
	if len(item.ConfigJson) > 0 {
		if err := json.Unmarshal(item.ConfigJson, &config); err != nil {
			return triggerToolView{}, fmt.Errorf("decode trigger config: %w", err)
		}
	}

	environmentSlug := ""
	if item.EnvironmentID.Valid {
		environment, err := envQueries.GetEnvironmentByID(ctx, environmentsrepo.GetEnvironmentByIDParams{
			ID:        item.EnvironmentID.UUID,
			ProjectID: projectID,
		})
		if err != nil {
			return triggerToolView{}, fmt.Errorf("get environment by id: %w", err)
		}
		environmentSlug = environment.Slug
	}

	var webhookURL *string
	if app != nil {
		webhookURL = app.WebhookURL(item)
	}

	return triggerToolView{
		ID:              item.ID.String(),
		DefinitionSlug:  item.DefinitionSlug,
		Name:            item.Name,
		EnvironmentID:   conv.Ternary(item.EnvironmentID.Valid, item.EnvironmentID.UUID.String(), ""),
		EnvironmentSlug: environmentSlug,
		TargetKind:      item.TargetKind,
		TargetRef:       item.TargetRef,
		TargetDisplay:   item.TargetDisplay,
		Status:          item.Status,
		Config:          config,
		WebhookURL:      webhookURL,
		CreatedAt:       item.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:       item.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}, nil
}

func buildTriggerInstanceSnapshot(item triggerrepo.TriggerInstance, webhookURL *string) (*types.TriggerInstance, error) {
	config := map[string]any{}
	if len(item.ConfigJson) > 0 {
		if err := json.Unmarshal(item.ConfigJson, &config); err != nil {
			return nil, fmt.Errorf("decode trigger config: %w", err)
		}
		if config == nil {
			config = map[string]any{}
		}
	}

	return &types.TriggerInstance{
		ID:             item.ID.String(),
		ProjectID:      item.ProjectID.String(),
		DefinitionSlug: item.DefinitionSlug,
		Name:           item.Name,
		EnvironmentID:  conv.Ternary(item.EnvironmentID.Valid, conv.PtrEmpty(item.EnvironmentID.UUID.String()), nil),
		TargetKind:     item.TargetKind,
		TargetRef:      item.TargetRef,
		TargetDisplay:  item.TargetDisplay,
		Config:         config,
		Status:         item.Status,
		WebhookURL:     webhookURL,
		CreatedAt:      item.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      item.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func normalizeStatus(status *string) string {
	if status == nil || strings.TrimSpace(*status) == "" {
		return triggerStatusActive
	}
	return strings.TrimSpace(*status)
}

func normalizeTargetKind(targetKind string) (string, error) {
	if strings.TrimSpace(targetKind) == "" {
		return "", fmt.Errorf("target_kind is required")
	}
	normalized := strings.TrimSpace(targetKind)
	switch normalized {
	case targetKindAssistant, targetKindNoop:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported target_kind %q", normalized)
	}
}

func decodePayload(payload io.Reader, target any) error {
	body, err := io.ReadAll(payload)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func requireProjectAuthContext(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, fmt.Errorf("platform tool requires project auth context")
	}
	return authCtx, nil
}

func triggerToolAnnotations(readOnly, destructive, idempotent, openWorld bool) *types.ToolAnnotations {
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}
