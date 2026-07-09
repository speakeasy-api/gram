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
	ActionGcpIamCredentialCreate Action = "gcp_iam_credential:create"
	ActionGcpIamCredentialUpdate Action = "gcp_iam_credential:update"
	ActionGcpIamCredentialDelete Action = "gcp_iam_credential:delete"
)

// GcpIamCredentialSnapshot is the audited state of a GCP IAM external
// credential.
type GcpIamCredentialSnapshot struct {
	Name                      string `json:"name"`
	ImpersonateServiceAccount string `json:"impersonate_service_account,omitempty"`
	WifPoolID                 string `json:"wif_pool_id,omitempty"`
	WifProviderID             string `json:"wif_provider_id,omitempty"`
	WifProjectNumber          string `json:"wif_project_number,omitempty"`
}

type LogGcpIamCredentialCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CredentialURN  urn.GcpIamCredential
	CredentialName string
}

func (l *Logger) LogGcpIamCredentialCreate(ctx context.Context, dbtx repo.DBTX, event LogGcpIamCredentialCreateEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionGcpIamCredentialCreate),

		SubjectID:          event.CredentialURN.ID.String(),
		SubjectType:        string(subjectTypeGcpIamCredential),
		SubjectDisplayName: conv.ToPGTextEmpty(event.CredentialName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.GcpIamCredentialV1})
}

type LogGcpIamCredentialUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CredentialURN  urn.GcpIamCredential
	CredentialName string

	CredentialSnapshotBefore *GcpIamCredentialSnapshot
	CredentialSnapshotAfter  *GcpIamCredentialSnapshot
}

func (l *Logger) LogGcpIamCredentialUpdate(ctx context.Context, dbtx repo.DBTX, event LogGcpIamCredentialUpdateEvent) error {
	action := ActionGcpIamCredentialUpdate

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
		SubjectType:        string(subjectTypeGcpIamCredential),
		SubjectDisplayName: conv.ToPGTextEmpty(event.CredentialName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: before,
		AfterSnapshot:  after,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.GcpIamCredentialV1})
}

type LogGcpIamCredentialDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	CredentialURN  urn.GcpIamCredential
	CredentialName string
}

func (l *Logger) LogGcpIamCredentialDelete(ctx context.Context, dbtx repo.DBTX, event LogGcpIamCredentialDeleteEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionGcpIamCredentialDelete),

		SubjectID:          event.CredentialURN.ID.String(),
		SubjectType:        string(subjectTypeGcpIamCredential),
		SubjectDisplayName: conv.ToPGTextEmpty(event.CredentialName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.GcpIamCredentialV1})
}
