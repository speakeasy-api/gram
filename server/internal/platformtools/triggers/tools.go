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
	sourceTriggers       = "triggers"
	toolNameListTriggers = "platform_list_triggers"
	toolNameConfigure    = "platform_configure_trigger"
	targetKindAssistant  = bgtriggers.TargetKindAssistant
	targetKindNoop       = bgtriggers.TargetKindNoop
	triggerStatusActive  = bgtriggers.StatusActive
	triggerStatusPaused  = bgtriggers.StatusPaused
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
	db  *pgxpool.Pool
	app *bgtriggers.App
}

type ConfigureTrigger struct {
	db          *pgxpool.Pool
	app         *bgtriggers.App
	inputSchema []byte
	audit       *audit.Logger
}

func NewListTriggersTool(db *pgxpool.Pool, app *bgtriggers.App) *ListTriggers {
	return &ListTriggers{
		db:  db,
		app: app,
	}
}

func NewConfigureTriggerTool(db *pgxpool.Pool, app *bgtriggers.App, audit *audit.Logger) *ConfigureTrigger {
	return &ConfigureTrigger{
		db:          db,
		app:         app,
		inputSchema: buildConfigureTriggerInputSchema(),
		audit:       audit,
	}
}

func buildConfigureTriggerInputSchema() []byte {
	definitionSlugs := listDefinitionSlugs()
	baseSchema := schemaBytesToMap(core.BuildInputSchema[configureTriggerSharedInput](
		core.WithPropertyFormat("trigger_id", "uuid"),
		core.WithPropertyEnum("definition_slug", stringSliceToAny(definitionSlugs)...),
		core.WithPropertyEnum("status", triggerStatusActive, triggerStatusPaused),
		core.WithPropertyEnum("target_kind", targetKindAssistant, targetKindNoop),
	))

	properties := getMap(baseSchema, "properties")
	properties["config"] = map[string]any{
		"type":        "object",
		"description": "Trigger-definition-specific configuration.",
	}

	required := append(getStringSlice(baseSchema, "required"), "config")
	baseSchema["required"] = dedupeStrings(required)

	branches := make([]any, 0, len(definitionSlugs))
	for _, slug := range definitionSlugs {
		definition, ok := bgtriggers.GetDefinition(slug)
		if !ok {
			panic(fmt.Errorf("missing trigger definition %q", slug))
		}
		branches = append(branches, map[string]any{
			"properties": map[string]any{
				"definition_slug": map[string]any{
					"const": slug,
				},
				"config": schemaBytesToMap(definition.ConfigSchema),
			},
			"required": []string{"definition_slug", "config"},
		})
	}
	baseSchema["oneOf"] = branches

	return mustMarshalSchemaMap(baseSchema)
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

	var input configureTriggerInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	result, err := t.upsertTrigger(ctx, authCtx, configureTriggerParams(input))
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
