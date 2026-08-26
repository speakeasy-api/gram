package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionExploreSavedQueryCreate Action = "explore_saved_query:create"
	ActionExploreSavedQueryUpdate Action = "explore_saved_query:update"
	ActionExploreSavedQueryDelete Action = "explore_saved_query:delete"
)

type ExploreSavedQuerySnapshot struct {
	Name       string          `json:"name"`
	ChartType  string          `json:"chart_type"`
	TimeWindow string          `json:"time_window"`
	Spec       json.RawMessage `json:"spec"`
}

type LogExploreSavedQueryCreateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ExploreSavedQueryURN  urn.ExploreSavedQuery
	ExploreSavedQueryName string
}

func (l *Logger) LogExploreSavedQueryCreate(ctx context.Context, dbtx repo.DBTX, event LogExploreSavedQueryCreateEvent) error {
	entry := exploreSavedQueryAuditEntry(
		event.OrganizationID,
		event.Actor,
		event.ActorDisplayName,
		event.ActorSlug,
		ActionExploreSavedQueryCreate,
		event.ExploreSavedQueryURN,
		event.ExploreSavedQueryName,
		nil,
		nil,
	)
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.ExploreSavedQueryV1})
}

type LogExploreSavedQueryUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ExploreSavedQueryURN            urn.ExploreSavedQuery
	ExploreSavedQueryName           string
	ExploreSavedQuerySnapshotBefore *ExploreSavedQuerySnapshot
	ExploreSavedQuerySnapshotAfter  *ExploreSavedQuerySnapshot
}

func (l *Logger) LogExploreSavedQueryUpdate(ctx context.Context, dbtx repo.DBTX, event LogExploreSavedQueryUpdateEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.ExploreSavedQuerySnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", ActionExploreSavedQueryUpdate, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.ExploreSavedQuerySnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", ActionExploreSavedQueryUpdate, err)
	}

	entry := exploreSavedQueryAuditEntry(
		event.OrganizationID,
		event.Actor,
		event.ActorDisplayName,
		event.ActorSlug,
		ActionExploreSavedQueryUpdate,
		event.ExploreSavedQueryURN,
		event.ExploreSavedQueryName,
		beforeSnapshot,
		afterSnapshot,
	)
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.ExploreSavedQueryV1})
}

type LogExploreSavedQueryDeleteEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ExploreSavedQueryURN  urn.ExploreSavedQuery
	ExploreSavedQueryName string
}

func (l *Logger) LogExploreSavedQueryDelete(ctx context.Context, dbtx repo.DBTX, event LogExploreSavedQueryDeleteEvent) error {
	entry := exploreSavedQueryAuditEntry(
		event.OrganizationID,
		event.Actor,
		event.ActorDisplayName,
		event.ActorSlug,
		ActionExploreSavedQueryDelete,
		event.ExploreSavedQueryURN,
		event.ExploreSavedQueryName,
		nil,
		nil,
	)
	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.ExploreSavedQueryV1})
}

func exploreSavedQueryAuditEntry(
	organizationID string,
	actor urn.Principal,
	actorDisplayName *string,
	actorSlug *string,
	action Action,
	queryURN urn.ExploreSavedQuery,
	queryName string,
	beforeSnapshot []byte,
	afterSnapshot []byte,
) repo.InsertAuditLogParams {
	return repo.InsertAuditLogParams{
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          actor.ID,
		ActorType:        string(actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(actorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(actorSlug),

		Action: string(action),

		SubjectID:          queryURN.ID.String(),
		SubjectType:        string(subjectTypeExploreSavedQuery),
		SubjectDisplayName: conv.ToPGTextEmpty(queryName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}
}
