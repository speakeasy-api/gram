package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionOtelForwardingUpsert Action = "otel_forwarding:upsert"
	ActionOtelForwardingDelete Action = "otel_forwarding:delete"
)

// OtelForwardingSnapshot is the public-facing shape persisted on update
// events. Header values are intentionally omitted — they are secrets and
// must never appear in the audit log.
type OtelForwardingSnapshot struct {
	EndpointURL string   `json:"endpoint_url"`
	HeaderNames []string `json:"header_names"`
	Enabled     bool     `json:"enabled"`
}

type LogOtelForwardingUpsertEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ConfigURN urn.OtelForwardingConfig

	SnapshotBefore *OtelForwardingSnapshot
	SnapshotAfter  *OtelForwardingSnapshot
}

func (l *Logger) LogOtelForwardingUpsert(ctx context.Context, dbtx repo.DBTX, event LogOtelForwardingUpsertEvent) error {
	action := ActionOtelForwardingUpsert

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
		SubjectType:        string(subjectTypeOtelForwarding),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, entry)
}

type LogOtelForwardingDeleteEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ConfigURN urn.OtelForwardingConfig
}

func (l *Logger) LogOtelForwardingDelete(ctx context.Context, dbtx repo.DBTX, event LogOtelForwardingDeleteEvent) error {
	action := ActionOtelForwardingDelete

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ConfigURN.ID.String(),
		SubjectType:        string(subjectTypeOtelForwarding),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, entry)
}
