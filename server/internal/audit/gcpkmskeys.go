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
	ActionGcpKmsKeyCreate Action = "gcp_kms_key:create"
	ActionGcpKmsKeyUpdate Action = "gcp_kms_key:update"
	ActionGcpKmsKeyDelete Action = "gcp_kms_key:delete"
)

// GcpKmsKeySnapshot is the audited state of a GCP KMS external key. Keys carry
// no bearer secret, so every field is recorded as-is.
type GcpKmsKeySnapshot struct {
	Name                   string `json:"name"`
	ExternalCredentialID   string `json:"external_credential_id"`
	Algorithm              string `json:"algorithm"`
	CustomerGrantReference string `json:"customer_grant_reference"`
	ResourceName           string `json:"resource_name"`
}

type LogGcpKmsKeyCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN  urn.GcpKmsKey
	KeyName string
}

func (l *Logger) LogGcpKmsKeyCreate(ctx context.Context, dbtx repo.DBTX, event LogGcpKmsKeyCreateEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionGcpKmsKeyCreate),

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeGcpKmsKey),
		SubjectDisplayName: conv.ToPGTextEmpty(event.KeyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.GcpKmsKeyV1})
}

type LogGcpKmsKeyUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN  urn.GcpKmsKey
	KeyName string

	KeySnapshotBefore *GcpKmsKeySnapshot
	KeySnapshotAfter  *GcpKmsKeySnapshot
}

func (l *Logger) LogGcpKmsKeyUpdate(ctx context.Context, dbtx repo.DBTX, event LogGcpKmsKeyUpdateEvent) error {
	action := ActionGcpKmsKeyUpdate

	before, err := marshalAuditPayload(event.KeySnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	after, err := marshalAuditPayload(event.KeySnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeGcpKmsKey),
		SubjectDisplayName: conv.ToPGTextEmpty(event.KeyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: before,
		AfterSnapshot:  after,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.GcpKmsKeyV1})
}

type LogGcpKmsKeyDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN  urn.GcpKmsKey
	KeyName string
}

func (l *Logger) LogGcpKmsKeyDelete(ctx context.Context, dbtx repo.DBTX, event LogGcpKmsKeyDeleteEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionGcpKmsKeyDelete),

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeGcpKmsKey),
		SubjectDisplayName: conv.ToPGTextEmpty(event.KeyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.GcpKmsKeyV1})
}
