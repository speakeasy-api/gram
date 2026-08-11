package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionAIIntegrationUpsert         Action = "ai_integration:upsert"
	ActionAIIntegrationDelete         Action = "ai_integration:delete"
	ActionAIIntegrationUpdateSchedule Action = "ai_integration:update_schedule"
	ActionAIIntegrationRetrySchedule  Action = "ai_integration:retry_schedule"
)

// AIIntegrationScheduleMetadata identifies which of a config's sync schedules
// an event acted on. Enabled is only set for update_schedule events.
type AIIntegrationScheduleMetadata struct {
	Provider string `json:"provider"`
	Schedule string `json:"schedule"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

// AIIntegrationSnapshot intentionally omits the API key. It only records
// whether a key was configured so audit consumers can see secret lifecycle
// changes without exposing the secret itself.
type AIIntegrationSnapshot struct {
	Provider  string    `json:"provider"`
	ProjectID uuid.UUID `json:"project_id"`
	Enabled   bool      `json:"enabled"`
	HasAPIKey bool      `json:"has_api_key"`
	// BillingMode records the declared billing mode ("metered", "flat_rate",
	// "unknown", or empty when undeclared). Changing it flips whether dashboard
	// cost reads as real spend or an estimate, so declarations must be auditable.
	BillingMode string `json:"billing_mode"`
}

type LogAIIntegrationUpsertEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ConfigURN urn.AIIntegrationConfig

	SnapshotBefore *AIIntegrationSnapshot
	SnapshotAfter  *AIIntegrationSnapshot
}

func (l *Logger) LogAIIntegrationUpsert(ctx context.Context, dbtx repo.DBTX, event LogAIIntegrationUpsertEvent) error {
	action := ActionAIIntegrationUpsert

	beforeSnapshot, err := marshalAuditPayload(event.SnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.SnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: true},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ConfigURN.ID.String(),
		SubjectType:        string(subjectTypeAIIntegration),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AIIntegrationV1})
}

type LogAIIntegrationUpdateScheduleEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ConfigURN urn.AIIntegrationConfig

	Provider string
	Schedule string
	Enabled  bool
}

func (l *Logger) LogAIIntegrationUpdateSchedule(ctx context.Context, dbtx repo.DBTX, event LogAIIntegrationUpdateScheduleEvent) error {
	action := ActionAIIntegrationUpdateSchedule

	metadata, err := marshalAuditPayload(&AIIntegrationScheduleMetadata{
		Provider: event.Provider,
		Schedule: event.Schedule,
		Enabled:  &event.Enabled,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: true},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ConfigURN.ID.String(),
		SubjectType:        string(subjectTypeAIIntegration),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AIIntegrationV1})
}

type LogAIIntegrationRetryScheduleEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ConfigURN urn.AIIntegrationConfig

	Provider string
	Schedule string
}

func (l *Logger) LogAIIntegrationRetrySchedule(ctx context.Context, dbtx repo.DBTX, event LogAIIntegrationRetryScheduleEvent) error {
	action := ActionAIIntegrationRetrySchedule

	metadata, err := marshalAuditPayload(&AIIntegrationScheduleMetadata{
		Provider: event.Provider,
		Schedule: event.Schedule,
		Enabled:  nil,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: true},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ConfigURN.ID.String(),
		SubjectType:        string(subjectTypeAIIntegration),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AIIntegrationV1})
}

type LogAIIntegrationDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ConfigURN urn.AIIntegrationConfig
}

func (l *Logger) LogAIIntegrationDelete(ctx context.Context, dbtx repo.DBTX, event LogAIIntegrationDeleteEvent) error {
	action := ActionAIIntegrationDelete

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: true},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ConfigURN.ID.String(),
		SubjectType:        string(subjectTypeAIIntegration),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AIIntegrationV1})
}
