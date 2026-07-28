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
	ActionModelProviderKeyUpsert Action = "model_provider_key:upsert"
	ActionModelProviderKeyDelete Action = "model_provider_key:delete"
)

// ModelProviderKeySnapshot intentionally omits the key material. It records
// the slot binding and enablement so audit consumers can follow secret
// lifecycle changes without exposing the secret itself.
type ModelProviderKeySnapshot struct {
	ProjectID uuid.UUID `json:"project_id"`
	Slot      string    `json:"slot"`
	Provider  string    `json:"provider"`
	Enabled   bool      `json:"enabled"`
}

type LogModelProviderKeyUpsertEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN urn.ModelProviderKey
	Slot   string

	SnapshotBefore *ModelProviderKeySnapshot
	SnapshotAfter  *ModelProviderKeySnapshot
}

func (l *Logger) LogModelProviderKeyUpsert(ctx context.Context, dbtx repo.DBTX, event LogModelProviderKeyUpsertEvent) error {
	action := ActionModelProviderKeyUpsert

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

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeModelProviderKey),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slot),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.ModelProviderKeyV1})
}

type LogModelProviderKeyDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN urn.ModelProviderKey
	Slot   string
}

func (l *Logger) LogModelProviderKeyDelete(ctx context.Context, dbtx repo.DBTX, event LogModelProviderKeyDeleteEvent) error {
	action := ActionModelProviderKeyDelete

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: true},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeModelProviderKey),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slot),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.ModelProviderKeyV1})
}
