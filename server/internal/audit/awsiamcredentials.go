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
	ActionAwsIamCredentialCreate Action = "aws_iam_credential:create"
	ActionAwsIamCredentialUpdate Action = "aws_iam_credential:update"
	ActionAwsIamCredentialDelete Action = "aws_iam_credential:delete"
)

// AwsIamCredentialSnapshot is the audited state of an AWS IAM external
// credential. It intentionally omits the raw AWS ExternalId — that value is a
// confused-deputy nonce and is never recorded in the audit log. Only whether
// one is configured is captured, via HasExternalID.
type AwsIamCredentialSnapshot struct {
	Name          string `json:"name"`
	AssumeRoleArn string `json:"assume_role_arn,omitempty"`
	HasExternalID bool   `json:"has_external_id"`
	OidcAudience  string `json:"oidc_audience,omitempty"`
	OidcSubject   string `json:"oidc_subject,omitempty"`
	StsRegion     string `json:"sts_region,omitempty"`
}

type LogAwsIamCredentialCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CredentialURN  urn.AwsIamCredential
	CredentialName string
}

func (l *Logger) LogAwsIamCredentialCreate(ctx context.Context, dbtx repo.DBTX, event LogAwsIamCredentialCreateEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionAwsIamCredentialCreate),

		SubjectID:          event.CredentialURN.ID.String(),
		SubjectType:        string(subjectTypeAwsIamCredential),
		SubjectDisplayName: conv.ToPGTextEmpty(event.CredentialName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AwsIamCredentialV1})
}

type LogAwsIamCredentialUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CredentialURN  urn.AwsIamCredential
	CredentialName string

	CredentialSnapshotBefore *AwsIamCredentialSnapshot
	CredentialSnapshotAfter  *AwsIamCredentialSnapshot
}

func (l *Logger) LogAwsIamCredentialUpdate(ctx context.Context, dbtx repo.DBTX, event LogAwsIamCredentialUpdateEvent) error {
	action := ActionAwsIamCredentialUpdate

	before, err := marshalAuditPayload(event.CredentialSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	after, err := marshalAuditPayload(event.CredentialSnapshotAfter)
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

		SubjectID:          event.CredentialURN.ID.String(),
		SubjectType:        string(subjectTypeAwsIamCredential),
		SubjectDisplayName: conv.ToPGTextEmpty(event.CredentialName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: before,
		AfterSnapshot:  after,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AwsIamCredentialV1})
}

type LogAwsIamCredentialDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CredentialURN  urn.AwsIamCredential
	CredentialName string
}

func (l *Logger) LogAwsIamCredentialDelete(ctx context.Context, dbtx repo.DBTX, event LogAwsIamCredentialDeleteEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionAwsIamCredentialDelete),

		SubjectID:          event.CredentialURN.ID.String(),
		SubjectType:        string(subjectTypeAwsIamCredential),
		SubjectDisplayName: conv.ToPGTextEmpty(event.CredentialName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AwsIamCredentialV1})
}
