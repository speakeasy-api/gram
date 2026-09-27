package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/explore"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionQueryCreate Action = "query:create"
	ActionQueryUpdate Action = "query:update"
	ActionQueryDelete Action = "query:delete"
)

// QueryEventBase is what every saved-query event carries: who did it, in
// which project, to which query.
type QueryEventBase struct {
	OrganizationID   string
	ProjectID        uuid.UUID
	Actor            urn.Principal
	ActorDisplayName *string
	QueryURN         urn.Query
	Name             string
}

type LogQueryCreateEvent struct {
	QueryEventBase
	Snapshot *gen.ExploreQuery
}

type LogQueryUpdateEvent struct {
	QueryEventBase
	Before *gen.ExploreQuery
	After  *gen.ExploreQuery
}

type LogQueryDeleteEvent struct{ QueryEventBase }

func queryEntry(base QueryEventBase, action Action, before, after []byte) repo.InsertAuditLogParams {
	return repo.InsertAuditLogParams{
		OrganizationID:     base.OrganizationID,
		ProjectID:          uuid.NullUUID{UUID: base.ProjectID, Valid: base.ProjectID != uuid.Nil},
		ActorID:            base.Actor.ID,
		ActorType:          string(base.Actor.Type),
		ActorDisplayName:   conv.PtrToPGTextEmpty(base.ActorDisplayName),
		ActorSlug:          conv.ToPGTextEmpty(""),
		Action:             string(action),
		SubjectID:          base.QueryURN.ID.String(),
		SubjectType:        string(subjectTypeQuery),
		SubjectDisplayName: conv.ToPGTextEmpty(base.Name),
		SubjectSlug:        conv.ToPGTextEmpty(""),
		Metadata:           nil,
		BeforeSnapshot:     before,
		AfterSnapshot:      after,
	}
}

func (l *Logger) LogQueryCreate(ctx context.Context, dbtx repo.DBTX, event LogQueryCreateEvent) error {
	after, err := marshalAuditPayload(event.Snapshot)
	if err != nil {
		return fmt.Errorf("marshal query create snapshot: %w", err)
	}
	return l.log(ctx, dbtx, auditEntry{Params: queryEntry(event.QueryEventBase, ActionQueryCreate, nil, after), OutboxEvent: events.QueryV1})
}

func (l *Logger) LogQueryUpdate(ctx context.Context, dbtx repo.DBTX, event LogQueryUpdateEvent) error {
	before, err := marshalAuditPayload(event.Before)
	if err != nil {
		return fmt.Errorf("marshal query update before snapshot: %w", err)
	}
	after, err := marshalAuditPayload(event.After)
	if err != nil {
		return fmt.Errorf("marshal query update after snapshot: %w", err)
	}
	return l.log(ctx, dbtx, auditEntry{Params: queryEntry(event.QueryEventBase, ActionQueryUpdate, before, after), OutboxEvent: events.QueryV1})
}

func (l *Logger) LogQueryDelete(ctx context.Context, dbtx repo.DBTX, event LogQueryDeleteEvent) error {
	return l.log(ctx, dbtx, auditEntry{Params: queryEntry(event.QueryEventBase, ActionQueryDelete, nil, nil), OutboxEvent: events.QueryV1})
}
