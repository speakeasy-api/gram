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
	ActionDataExportRouteCreate Action = "data_export_route:create"
	ActionDataExportRouteUpdate Action = "data_export_route:update"
	ActionDataExportRouteDelete Action = "data_export_route:delete"
	ActionDataExportRoutePause  Action = "data_export_route:pause"
	ActionDataExportRouteResume Action = "data_export_route:resume"
)

type DataExportRouteSnapshot struct {
	DataSource        string  `json:"data_source"`
	Enabled           bool    `json:"enabled"`
	OtelDestinationID *string `json:"otel_destination_id,omitempty"`
}

type LogDataExportRouteCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RouteURN   urn.DataExportRoute
	DataSource string
}

func (l *Logger) LogDataExportRouteCreate(ctx context.Context, dbtx repo.DBTX, event LogDataExportRouteCreateEvent) error {
	action := ActionDataExportRouteCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RouteURN.ID.String(),
		SubjectType:        string(subjectTypeDataExportRoute),
		SubjectDisplayName: conv.ToPGTextEmpty(event.DataSource),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.DataExportRouteV1})
}

type LogDataExportRouteUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RouteURN            urn.DataExportRoute
	DataSource          string
	RouteSnapshotBefore *DataExportRouteSnapshot
	RouteSnapshotAfter  *DataExportRouteSnapshot
}

func (l *Logger) LogDataExportRouteUpdate(ctx context.Context, dbtx repo.DBTX, event LogDataExportRouteUpdateEvent) error {
	action := ActionDataExportRouteUpdate
	beforeSnapshot, err := marshalAuditPayload(event.RouteSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.RouteSnapshotAfter)
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

		SubjectID:          event.RouteURN.ID.String(),
		SubjectType:        string(subjectTypeDataExportRoute),
		SubjectDisplayName: conv.ToPGTextEmpty(event.DataSource),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.DataExportRouteV1})
}

type LogDataExportRouteDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RouteURN   urn.DataExportRoute
	DataSource string
}

func (l *Logger) LogDataExportRouteDelete(ctx context.Context, dbtx repo.DBTX, event LogDataExportRouteDeleteEvent) error {
	action := ActionDataExportRouteDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RouteURN.ID.String(),
		SubjectType:        string(subjectTypeDataExportRoute),
		SubjectDisplayName: conv.ToPGTextEmpty(event.DataSource),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.DataExportRouteV1})
}

// LogDataExportRoutePauseEvent records a route's export being stopped without
// any other change to the route. The snapshots show that only the enabled flag
// moved.
type LogDataExportRoutePauseEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RouteURN            urn.DataExportRoute
	DataSource          string
	RouteSnapshotBefore *DataExportRouteSnapshot
	RouteSnapshotAfter  *DataExportRouteSnapshot
}

func (l *Logger) LogDataExportRoutePause(ctx context.Context, dbtx repo.DBTX, event LogDataExportRoutePauseEvent) error {
	return l.logDataExportRouteToggle(ctx, dbtx, ActionDataExportRoutePause, dataExportRouteToggle(event))
}

// LogDataExportRouteResumeEvent records a paused route starting to export
// again. The snapshots show that only the enabled flag moved.
type LogDataExportRouteResumeEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RouteURN            urn.DataExportRoute
	DataSource          string
	RouteSnapshotBefore *DataExportRouteSnapshot
	RouteSnapshotAfter  *DataExportRouteSnapshot
}

func (l *Logger) LogDataExportRouteResume(ctx context.Context, dbtx repo.DBTX, event LogDataExportRouteResumeEvent) error {
	return l.logDataExportRouteToggle(ctx, dbtx, ActionDataExportRouteResume, dataExportRouteToggle(event))
}

// dataExportRouteToggle is the field set pause and resume share. Both event
// types convert to it, so the two actions cannot record different shapes.
type dataExportRouteToggle LogDataExportRoutePauseEvent

func (l *Logger) logDataExportRouteToggle(ctx context.Context, dbtx repo.DBTX, action Action, event dataExportRouteToggle) error {
	beforeSnapshot, err := marshalAuditPayload(event.RouteSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.RouteSnapshotAfter)
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

		SubjectID:          event.RouteURN.ID.String(),
		SubjectType:        string(subjectTypeDataExportRoute),
		SubjectDisplayName: conv.ToPGTextEmpty(event.DataSource),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.DataExportRouteV1})
}
