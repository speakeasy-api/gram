package triggers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	toolNameScheduleWake = "platform_schedule_wake"
	toolNameCancelWake   = "platform_cancel_wake"
)

type scheduleWakeInput struct {
	FireAt string  `json:"fire_at" jsonschema:"Absolute future time at which to wake the thread. RFC3339 timestamp, max 30 days ahead."`
	Note   *string `json:"note,omitempty" jsonschema:"Optional self-note surfaced on wake so the assistant knows why it woke."`
}

type scheduleWakeResult struct {
	TriggerInstanceID string `json:"trigger_instance_id"`
	FireAt            string `json:"fire_at"`
}

type cancelWakeInput struct {
	TriggerInstanceID string `json:"trigger_instance_id" jsonschema:"The wake trigger instance ID to cancel."`
}

type cancelWakeResult struct {
	TriggerInstanceID string `json:"trigger_instance_id"`
	Status            string `json:"status"`
}

type ScheduleWake struct {
	db    *pgxpool.Pool
	app   *bgtriggers.App
	audit *audit.Logger
}

type CancelWake struct {
	db    *pgxpool.Pool
	app   *bgtriggers.App
	audit *audit.Logger
}

func NewScheduleWakeTool(db *pgxpool.Pool, app *bgtriggers.App, audit *audit.Logger) *ScheduleWake {
	return &ScheduleWake{db: db, app: app, audit: audit}
}

func NewCancelWakeTool(db *pgxpool.Pool, app *bgtriggers.App, audit *audit.Logger) *CancelWake {
	return &CancelWake{db: db, app: app, audit: audit}
}

func (t *ScheduleWake) Descriptor() core.ToolDescriptor {
	readOnly := false
	destructive := false
	idempotent := false
	openWorld := false
	return core.ToolDescriptor{
		SourceSlug:  sourceTriggers,
		HandlerName: "schedule_wake",
		Name:        toolNameScheduleWake,
		Description: "Schedule a one-shot future wake-up for the current assistant thread. Use to remind yourself to retry, follow up, or revisit work later. Max 30 days ahead.",
		InputSchema: core.BuildInputSchema[scheduleWakeInput](
			core.WithPropertyFormat("fire_at", "date-time"),
		),
		Annotations: triggerToolAnnotations(readOnly, destructive, idempotent, openWorld),
		Managed:     true,
		Variables:   nil,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *ScheduleWake) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.db == nil || t.app == nil {
		return fmt.Errorf("wake tools are not configured")
	}

	principal, ok := contextvalues.GetAssistantPrincipal(ctx)
	if !ok {
		return oops.E(oops.CodeUnauthorized, nil, "wake tools require an assistant principal")
	}

	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return err
	}

	var input scheduleWakeInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	fireAt, err := time.Parse(time.RFC3339, strings.TrimSpace(input.FireAt))
	if err != nil {
		return fmt.Errorf("parse fire_at: %w", err)
	}

	thread, err := assistantrepo.New(t.db).ResolveThreadCorrelation(ctx, principal.ThreadID)
	if err != nil {
		return fmt.Errorf("resolve thread correlation: %w", err)
	}
	if thread.ProjectID != *authCtx.ProjectID || thread.AssistantID != principal.AssistantID {
		return oops.E(oops.CodeForbidden, nil, "thread does not belong to the calling assistant")
	}

	actorPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)

	instance, err := t.app.CreateWakeInstance(ctx, bgtriggers.CreateWakeInstanceParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "wake-" + fireAt.UTC().Format("20060102T150405Z"),
		AssistantID:    principal.AssistantID,
		TargetDisplay:  principal.AssistantID.String() + ":" + principal.ThreadID.String(),
		FireAt:         fireAt.UTC(),
		Note:           input.Note,
		CorrelationID:  thread.CorrelationID,
	}, func(ctx context.Context, dbtx pgx.Tx, item triggerrepo.TriggerInstance) error {
		if t.audit == nil {
			return nil
		}
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
		return fmt.Errorf("create wake instance: %w", err)
	}

	result := scheduleWakeResult{
		TriggerInstanceID: instance.ID.String(),
		FireAt:            fireAt.UTC().Format(time.RFC3339Nano),
	}
	if err := json.NewEncoder(wr).Encode(result); err != nil {
		return fmt.Errorf("encode schedule wake result: %w", err)
	}
	return nil
}

func (t *CancelWake) Descriptor() core.ToolDescriptor {
	readOnly := false
	destructive := true
	idempotent := true
	openWorld := false
	return core.ToolDescriptor{
		SourceSlug:  sourceTriggers,
		HandlerName: "cancel_wake",
		Name:        toolNameCancelWake,
		Description: "Cancel a pending wake trigger that the current assistant thread previously scheduled.",
		InputSchema: core.BuildInputSchema[cancelWakeInput](
			core.WithPropertyFormat("trigger_instance_id", "uuid"),
		),
		Annotations: triggerToolAnnotations(readOnly, destructive, idempotent, openWorld),
		Managed:     true,
		Variables:   nil,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (t *CancelWake) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.db == nil || t.app == nil {
		return fmt.Errorf("wake tools are not configured")
	}

	principal, ok := contextvalues.GetAssistantPrincipal(ctx)
	if !ok {
		return oops.E(oops.CodeUnauthorized, nil, "wake tools require an assistant principal")
	}

	authCtx, err := requireProjectAuthContext(ctx)
	if err != nil {
		return err
	}

	var input cancelWakeInput
	if err := decodePayload(payload, &input); err != nil {
		return err
	}

	instanceID, err := uuid.Parse(strings.TrimSpace(input.TriggerInstanceID))
	if err != nil {
		return fmt.Errorf("parse trigger_instance_id: %w", err)
	}

	existing, err := t.app.GetInstance(ctx, *authCtx.ProjectID, instanceID)
	if err != nil {
		return fmt.Errorf("get wake instance: %w", err)
	}
	if existing.DefinitionSlug != bgtriggers.DefinitionSlugWake {
		return oops.E(oops.CodeBadRequest, nil, "trigger is not a wake")
	}
	if existing.TargetKind != bgtriggers.TargetKindAssistant || existing.TargetRef != principal.AssistantID.String() {
		return oops.E(oops.CodeForbidden, nil, "wake does not belong to the calling assistant")
	}

	correlationID, fireAt := bgtriggers.WakeConfigFields(existing.ConfigJson)

	actorPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID)

	item, err := t.app.CancelWakeInstance(ctx, *authCtx.ProjectID, instanceID, func(ctx context.Context, dbtx pgx.Tx, instance triggerrepo.TriggerInstance) error {
		if t.audit == nil {
			return nil
		}
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
		return fmt.Errorf("cancel wake instance: %w", err)
	}

	result := cancelWakeResult{
		TriggerInstanceID: item.ID.String(),
		Status:            item.Status,
	}
	if err := json.NewEncoder(wr).Encode(result); err != nil {
		return fmt.Errorf("encode cancel wake result: %w", err)
	}
	return nil
}
