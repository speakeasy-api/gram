package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/dashboards"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionDashboardCreate Action = "dashboard:create"
	// ActionDashboardUpdate covers a change to a dashboard's name,
	// description or saved filters.
	ActionDashboardUpdate Action = "dashboard:update"
	// ActionDashboardLayout covers a change to a dashboard's cards: one
	// added, removed, moved or resized. Its snapshots are the cards alone,
	// because the grid autosaves every drag.
	ActionDashboardLayout Action = "dashboard:layout"
	ActionDashboardDelete Action = "dashboard:delete"
)

// DashboardEventBase is what every dashboard event carries: who did it, in
// which project, to which dashboard.
type DashboardEventBase struct {
	OrganizationID   string
	ProjectID        uuid.UUID
	Actor            urn.Principal
	ActorDisplayName *string
	DashboardURN     urn.Dashboard
	Name             string
}

type LogDashboardCreateEvent struct {
	DashboardEventBase
	Snapshot *gen.Dashboard
	// DuplicatedFrom is the dashboard this one was copied from, when it was
	// made by duplication rather than from nothing.
	DuplicatedFrom *urn.Dashboard
}

type LogDashboardUpdateEvent struct {
	DashboardEventBase
	Before *gen.Dashboard
	After  *gen.Dashboard
}

type LogDashboardLayoutEvent struct {
	DashboardEventBase
	Before []*gen.DashboardPlacement
	After  []*gen.DashboardPlacement
}

type LogDashboardDeleteEvent struct{ DashboardEventBase }

func dashboardEntry(base DashboardEventBase, action Action, metadata, before, after []byte) repo.InsertAuditLogParams {
	return repo.InsertAuditLogParams{
		OrganizationID:     base.OrganizationID,
		ProjectID:          uuid.NullUUID{UUID: base.ProjectID, Valid: base.ProjectID != uuid.Nil},
		ActorID:            base.Actor.ID,
		ActorType:          string(base.Actor.Type),
		ActorDisplayName:   conv.PtrToPGTextEmpty(base.ActorDisplayName),
		ActorSlug:          conv.ToPGTextEmpty(""),
		Action:             string(action),
		SubjectID:          base.DashboardURN.ID.String(),
		SubjectType:        string(subjectTypeDashboard),
		SubjectDisplayName: conv.ToPGTextEmpty(base.Name),
		SubjectSlug:        conv.ToPGTextEmpty(""),
		Metadata:           metadata,
		BeforeSnapshot:     before,
		AfterSnapshot:      after,
	}
}

func (l *Logger) LogDashboardCreate(ctx context.Context, dbtx repo.DBTX, event LogDashboardCreateEvent) error {
	after, err := marshalAuditPayload(event.Snapshot)
	if err != nil {
		return fmt.Errorf("marshal dashboard create snapshot: %w", err)
	}
	var metadata []byte
	if event.DuplicatedFrom != nil {
		metadata, err = marshalAuditPayload(map[string]any{"duplicated_from": event.DuplicatedFrom.String()})
		if err != nil {
			return fmt.Errorf("marshal dashboard create metadata: %w", err)
		}
	}
	return l.log(ctx, dbtx, auditEntry{Params: dashboardEntry(event.DashboardEventBase, ActionDashboardCreate, metadata, nil, after), OutboxEvent: events.DashboardV1})
}

func (l *Logger) LogDashboardUpdate(ctx context.Context, dbtx repo.DBTX, event LogDashboardUpdateEvent) error {
	before, err := marshalAuditPayload(event.Before)
	if err != nil {
		return fmt.Errorf("marshal dashboard update before snapshot: %w", err)
	}
	after, err := marshalAuditPayload(event.After)
	if err != nil {
		return fmt.Errorf("marshal dashboard update after snapshot: %w", err)
	}
	return l.log(ctx, dbtx, auditEntry{Params: dashboardEntry(event.DashboardEventBase, ActionDashboardUpdate, nil, before, after), OutboxEvent: events.DashboardV1})
}

func (l *Logger) LogDashboardLayout(ctx context.Context, dbtx repo.DBTX, event LogDashboardLayoutEvent) error {
	before, err := marshalAuditPayload(event.Before)
	if err != nil {
		return fmt.Errorf("marshal dashboard layout before snapshot: %w", err)
	}
	after, err := marshalAuditPayload(event.After)
	if err != nil {
		return fmt.Errorf("marshal dashboard layout after snapshot: %w", err)
	}
	return l.log(ctx, dbtx, auditEntry{Params: dashboardEntry(event.DashboardEventBase, ActionDashboardLayout, nil, before, after), OutboxEvent: events.DashboardV1})
}

func (l *Logger) LogDashboardDelete(ctx context.Context, dbtx repo.DBTX, event LogDashboardDeleteEvent) error {
	return l.log(ctx, dbtx, auditEntry{Params: dashboardEntry(event.DashboardEventBase, ActionDashboardDelete, nil, nil, nil), OutboxEvent: events.DashboardV1})
}
