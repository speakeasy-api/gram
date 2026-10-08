package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/widgets"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionWidgetCreate Action = "widget:create"
	ActionWidgetUpdate Action = "widget:update"
	ActionWidgetDelete Action = "widget:delete"
)

// WidgetEventBase is what every widget event carries: who did it, in which
// project, to which widget.
type WidgetEventBase struct {
	OrganizationID   string
	ProjectID        uuid.UUID
	Actor            urn.Principal
	ActorDisplayName *string
	WidgetURN        urn.Widget
	Name             string
}

type LogWidgetCreateEvent struct {
	WidgetEventBase
	Snapshot *gen.Widget
	// DuplicatedFrom is the widget this one was copied from, when it was
	// made by duplication rather than saved from the builder.
	DuplicatedFrom *urn.Widget
}

type LogWidgetUpdateEvent struct {
	WidgetEventBase
	Before *gen.Widget
	After  *gen.Widget
}

type LogWidgetDeleteEvent struct {
	WidgetEventBase
	// RemovedFrom names the dashboards the widget was taken off as it went.
	RemovedFrom []urn.Dashboard
}

func widgetEntry(base WidgetEventBase, action Action, metadata, before, after []byte) repo.InsertAuditLogParams {
	return repo.InsertAuditLogParams{
		OrganizationID:     base.OrganizationID,
		ProjectID:          uuid.NullUUID{UUID: base.ProjectID, Valid: base.ProjectID != uuid.Nil},
		ActorID:            base.Actor.ID,
		ActorType:          string(base.Actor.Type),
		ActorDisplayName:   conv.PtrToPGTextEmpty(base.ActorDisplayName),
		ActorSlug:          conv.ToPGTextEmpty(""),
		Action:             string(action),
		SubjectID:          base.WidgetURN.ID.String(),
		SubjectType:        string(subjectTypeWidget),
		SubjectDisplayName: conv.ToPGTextEmpty(base.Name),
		SubjectSlug:        conv.ToPGTextEmpty(""),
		Metadata:           metadata,
		BeforeSnapshot:     before,
		AfterSnapshot:      after,
	}
}

func (l *Logger) LogWidgetCreate(ctx context.Context, dbtx repo.DBTX, event LogWidgetCreateEvent) error {
	after, err := marshalAuditPayload(event.Snapshot)
	if err != nil {
		return fmt.Errorf("marshal widget create snapshot: %w", err)
	}
	var metadata []byte
	if event.DuplicatedFrom != nil {
		metadata, err = marshalAuditPayload(map[string]any{"duplicated_from": event.DuplicatedFrom.String()})
		if err != nil {
			return fmt.Errorf("marshal widget create metadata: %w", err)
		}
	}
	return l.log(ctx, dbtx, auditEntry{Params: widgetEntry(event.WidgetEventBase, ActionWidgetCreate, metadata, nil, after), OutboxEvent: events.WidgetV1})
}

func (l *Logger) LogWidgetUpdate(ctx context.Context, dbtx repo.DBTX, event LogWidgetUpdateEvent) error {
	before, err := marshalAuditPayload(event.Before)
	if err != nil {
		return fmt.Errorf("marshal widget update before snapshot: %w", err)
	}
	after, err := marshalAuditPayload(event.After)
	if err != nil {
		return fmt.Errorf("marshal widget update after snapshot: %w", err)
	}
	return l.log(ctx, dbtx, auditEntry{Params: widgetEntry(event.WidgetEventBase, ActionWidgetUpdate, nil, before, after), OutboxEvent: events.WidgetV1})
}

func (l *Logger) LogWidgetDelete(ctx context.Context, dbtx repo.DBTX, event LogWidgetDeleteEvent) error {
	var metadata []byte
	if len(event.RemovedFrom) > 0 {
		dashboards := make([]string, 0, len(event.RemovedFrom))
		for _, dashboard := range event.RemovedFrom {
			dashboards = append(dashboards, dashboard.String())
		}
		var err error
		metadata, err = marshalAuditPayload(map[string]any{"removed_from": dashboards})
		if err != nil {
			return fmt.Errorf("marshal widget delete metadata: %w", err)
		}
	}
	return l.log(ctx, dbtx, auditEntry{Params: widgetEntry(event.WidgetEventBase, ActionWidgetDelete, metadata, nil, nil), OutboxEvent: events.WidgetV1})
}
