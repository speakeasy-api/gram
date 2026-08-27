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
	ActionOtelDestinationCreate Action = "otel_destination:create"
	ActionOtelDestinationUpdate Action = "otel_destination:update"
	ActionOtelDestinationDelete Action = "otel_destination:delete"
)

type OtelDestinationHeaderSnapshot struct {
	Name     string `json:"name"`
	HasValue bool   `json:"has_value"`
}

type OtelDestinationSnapshot struct {
	EndpointURL   string                          `json:"endpoint_url"`
	Headers       []OtelDestinationHeaderSnapshot `json:"headers"`
	SensitiveData string                          `json:"sensitive_data"`
}

type LogOtelDestinationCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	DestinationURN urn.OtelDestination
	EndpointURL    string
}

func (l *Logger) LogOtelDestinationCreate(ctx context.Context, dbtx repo.DBTX, event LogOtelDestinationCreateEvent) error {
	action := ActionOtelDestinationCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.DestinationURN.ID.String(),
		SubjectType:        string(subjectTypeOtelDestination),
		SubjectDisplayName: conv.ToPGTextEmpty(event.EndpointURL),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OtelDestinationV1})
}

type LogOtelDestinationUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	DestinationURN            urn.OtelDestination
	EndpointURL               string
	DestinationSnapshotBefore *OtelDestinationSnapshot
	DestinationSnapshotAfter  *OtelDestinationSnapshot
}

func (l *Logger) LogOtelDestinationUpdate(ctx context.Context, dbtx repo.DBTX, event LogOtelDestinationUpdateEvent) error {
	action := ActionOtelDestinationUpdate
	beforeSnapshot, err := marshalAuditPayload(event.DestinationSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.DestinationSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.DestinationURN.ID.String(),
		SubjectType:        string(subjectTypeOtelDestination),
		SubjectDisplayName: conv.ToPGTextEmpty(event.EndpointURL),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OtelDestinationV1})
}

type LogOtelDestinationDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	DestinationURN urn.OtelDestination
	EndpointURL    string
}

func (l *Logger) LogOtelDestinationDelete(ctx context.Context, dbtx repo.DBTX, event LogOtelDestinationDeleteEvent) error {
	action := ActionOtelDestinationDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.DestinationURN.ID.String(),
		SubjectType:        string(subjectTypeOtelDestination),
		SubjectDisplayName: conv.ToPGTextEmpty(event.EndpointURL),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OtelDestinationV1})
}
