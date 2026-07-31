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
	ActionAwsKmsKeyCreate Action = "aws_kms_key:create"
	ActionAwsKmsKeyUpdate Action = "aws_kms_key:update"
	ActionAwsKmsKeyDelete Action = "aws_kms_key:delete"
)

// AwsKmsKeySnapshot is the audited state of an AWS KMS external key. Keys carry
// no bearer secret (unlike the AWS credential ExternalId), so every field is
// recorded as-is.
type AwsKmsKeySnapshot struct {
	Name                   string `json:"name"`
	ExternalCredentialID   string `json:"external_credential_id"`
	Algorithm              string `json:"algorithm"`
	CustomerGrantReference string `json:"customer_grant_reference"`
	KeyArn                 string `json:"key_arn"`
}

type LogAwsKmsKeyCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN  urn.AwsKmsKey
	KeyName string
}

func (l *Logger) LogAwsKmsKeyCreate(ctx context.Context, dbtx repo.DBTX, event LogAwsKmsKeyCreateEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionAwsKmsKeyCreate),

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeAwsKmsKey),
		SubjectDisplayName: conv.ToPGTextEmpty(event.KeyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AwsKmsKeyV1})
}

type LogAwsKmsKeyUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN  urn.AwsKmsKey
	KeyName string

	KeySnapshotBefore *AwsKmsKeySnapshot
	KeySnapshotAfter  *AwsKmsKeySnapshot
}

func (l *Logger) LogAwsKmsKeyUpdate(ctx context.Context, dbtx repo.DBTX, event LogAwsKmsKeyUpdateEvent) error {
	action := ActionAwsKmsKeyUpdate

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
		SubjectType:        string(subjectTypeAwsKmsKey),
		SubjectDisplayName: conv.ToPGTextEmpty(event.KeyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: before,
		AfterSnapshot:  after,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AwsKmsKeyV1})
}

type LogAwsKmsKeyDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN  urn.AwsKmsKey
	KeyName string
}

func (l *Logger) LogAwsKmsKeyDelete(ctx context.Context, dbtx repo.DBTX, event LogAwsKmsKeyDeleteEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionAwsKmsKeyDelete),

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeAwsKmsKey),
		SubjectDisplayName: conv.ToPGTextEmpty(event.KeyName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AwsKmsKeyV1})
}
