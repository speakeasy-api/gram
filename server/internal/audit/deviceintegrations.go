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
	ActionDeviceIntegrationUpsert         Action = "device_integration:upsert"
	ActionDeviceIntegrationDelete         Action = "device_integration:delete"
	ActionDeviceIntegrationUpdateSchedule Action = "device_integration:update_schedule"
	ActionDeviceIntegrationRetrySchedule  Action = "device_integration:retry_schedule"
)

// DeviceIntegrationScheduleMetadata identifies which of a config's sync
// schedules an event acted on. Enabled is only set for update_schedule
// events.
type DeviceIntegrationScheduleMetadata struct {
	Provider string `json:"provider"`
	Schedule string `json:"schedule"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

// DeviceIntegrationSnapshot intentionally omits credential values. It only
// records whether credentials are configured so audit consumers can see
// secret lifecycle changes without exposing the secrets themselves. Settings
// are the non-secret, admin-visible values (e.g. instance URL).
type DeviceIntegrationSnapshot struct {
	Provider       string            `json:"provider"`
	Enabled        bool              `json:"enabled"`
	HasCredentials bool              `json:"has_credentials"`
	Settings       map[string]string `json:"settings"`
}

type LogDeviceIntegrationUpsertEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ConfigURN urn.DeviceIntegrationConfig

	SnapshotBefore *DeviceIntegrationSnapshot
	SnapshotAfter  *DeviceIntegrationSnapshot
}

func (l *Logger) LogDeviceIntegrationUpsert(ctx context.Context, dbtx repo.DBTX, event LogDeviceIntegrationUpsertEvent) error {
	action := ActionDeviceIntegrationUpsert

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
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ConfigURN.ID.String(),
		SubjectType:        string(subjectTypeDeviceIntegration),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.DeviceIntegrationV1})
}

type LogDeviceIntegrationDeleteEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ConfigURN urn.DeviceIntegrationConfig
}

func (l *Logger) LogDeviceIntegrationDelete(ctx context.Context, dbtx repo.DBTX, event LogDeviceIntegrationDeleteEvent) error {
	action := ActionDeviceIntegrationDelete

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ConfigURN.ID.String(),
		SubjectType:        string(subjectTypeDeviceIntegration),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.DeviceIntegrationV1})
}

type LogDeviceIntegrationUpdateScheduleEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ConfigURN urn.DeviceIntegrationConfig

	Provider string
	Schedule string
	Enabled  bool
}

func (l *Logger) LogDeviceIntegrationUpdateSchedule(ctx context.Context, dbtx repo.DBTX, event LogDeviceIntegrationUpdateScheduleEvent) error {
	action := ActionDeviceIntegrationUpdateSchedule

	metadata, err := marshalAuditPayload(&DeviceIntegrationScheduleMetadata{
		Provider: event.Provider,
		Schedule: event.Schedule,
		Enabled:  &event.Enabled,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ConfigURN.ID.String(),
		SubjectType:        string(subjectTypeDeviceIntegration),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.DeviceIntegrationV1})
}

type LogDeviceIntegrationRetryScheduleEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ConfigURN urn.DeviceIntegrationConfig

	Provider string
	Schedule string
}

func (l *Logger) LogDeviceIntegrationRetrySchedule(ctx context.Context, dbtx repo.DBTX, event LogDeviceIntegrationRetryScheduleEvent) error {
	action := ActionDeviceIntegrationRetrySchedule

	metadata, err := marshalAuditPayload(&DeviceIntegrationScheduleMetadata{
		Provider: event.Provider,
		Schedule: event.Schedule,
		Enabled:  nil,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ConfigURN.ID.String(),
		SubjectType:        string(subjectTypeDeviceIntegration),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.DeviceIntegrationV1})
}
