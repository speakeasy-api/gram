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
	ActionOktaServerSuggestionDismiss Action = "okta-server-suggestion:dismiss"
	ActionOktaServerSuggestionRestore Action = "okta-server-suggestion:restore"
)

// OktaServerSuggestionSnapshot is the suggestion state an audit entry records:
// the catalog entry and the state the administrator saw, never tenant data.
type OktaServerSuggestionSnapshot struct {
	RegistryEntryID string   `json:"registry_entry_id"`
	ServerName      string   `json:"server_name"`
	OktaAppIDs      []string `json:"okta_app_ids"`
	State           string   `json:"state"`
}

type LogOktaServerSuggestionEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SuggestionURN  urn.OktaServerSuggestion
	ServerName     string
	SnapshotBefore *OktaServerSuggestionSnapshot
	SnapshotAfter  *OktaServerSuggestionSnapshot
}

func (l *Logger) LogOktaServerSuggestionDismiss(ctx context.Context, dbtx repo.DBTX, event LogOktaServerSuggestionEvent) error {
	return l.logOktaServerSuggestion(ctx, dbtx, ActionOktaServerSuggestionDismiss, event)
}

func (l *Logger) LogOktaServerSuggestionRestore(ctx context.Context, dbtx repo.DBTX, event LogOktaServerSuggestionEvent) error {
	return l.logOktaServerSuggestion(ctx, dbtx, ActionOktaServerSuggestionRestore, event)
}

func (l *Logger) logOktaServerSuggestion(ctx context.Context, dbtx repo.DBTX, action Action, event LogOktaServerSuggestionEvent) error {
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

		SubjectID:          event.SuggestionURN.ID.String(),
		SubjectType:        string(subjectTypeOktaServerSuggestion),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ServerName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OktaServerSuggestionV1})
}
