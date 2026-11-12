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
	ActionDataExportRouteTest   Action = "data_export_route:test"
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
		IsInternal:     false,
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

// LogDataExportRouteUpdateEvent describes a change to an existing route with
// its before and after state. Pause and resume record the same shape under
// their own actions, so one event type serves all three.
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
	return l.logDataExportRouteChange(ctx, dbtx, ActionDataExportRouteUpdate, event)
}

// LogDataExportRoutePause records a route's export being stopped. Its
// snapshots show that only the enabled flag moved.
func (l *Logger) LogDataExportRoutePause(ctx context.Context, dbtx repo.DBTX, event LogDataExportRouteUpdateEvent) error {
	return l.logDataExportRouteChange(ctx, dbtx, ActionDataExportRoutePause, event)
}

// LogDataExportRouteResume records a paused route exporting again. Its
// snapshots show that only the enabled flag moved.
func (l *Logger) LogDataExportRouteResume(ctx context.Context, dbtx repo.DBTX, event LogDataExportRouteUpdateEvent) error {
	return l.logDataExportRouteChange(ctx, dbtx, ActionDataExportRouteResume, event)
}

func (l *Logger) logDataExportRouteChange(ctx context.Context, dbtx repo.DBTX, action Action, event LogDataExportRouteUpdateEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.RouteSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.RouteSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		IsInternal:     false,
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
		IsInternal:     false,
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

// LogDataExportRouteTestEvent records one test event sent to a route's
// destination and what it got back. It never carries headers or the response body.
type LogDataExportRouteTestEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RouteURN       urn.DataExportRoute
	DataSource     string
	DestinationURN urn.OtelDestination
	Outcome        string
	// StatusCode is zero when no response came back.
	StatusCode int
	// Signals is each signal's own outcome, one per OTLP request the test sent.
	Signals []DataExportRouteTestSignal
}

// DataExportRouteTestSignal is what one signal's test request got back.
type DataExportRouteTestSignal struct {
	Signal     string `json:"signal"`
	Outcome    string `json:"outcome"`
	StatusCode int    `json:"status_code,omitempty"`
}

type dataExportRouteTestMetadata struct {
	DestinationID string                      `json:"otel_destination_id"`
	Outcome       string                      `json:"outcome"`
	StatusCode    int                         `json:"status_code,omitempty"`
	Signals       []DataExportRouteTestSignal `json:"signals,omitempty"`
}

func (l *Logger) LogDataExportRouteTest(ctx context.Context, dbtx repo.DBTX, event LogDataExportRouteTestEvent) error {
	action := ActionDataExportRouteTest
	metadata, err := marshalAuditPayload(dataExportRouteTestMetadata{DestinationID: event.DestinationURN.ID.String(), Outcome: event.Outcome, StatusCode: event.StatusCode, Signals: event.Signals})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}
	entry := repo.InsertAuditLogParams{
		IsInternal:     false,
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
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.DataExportRouteV1})
}
