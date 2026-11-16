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
	ActionOrganizationEncryptionConfigCreate         Action = "organization_encryption_config:create"
	ActionOrganizationEncryptionConfigSwitchKey      Action = "organization_encryption_config:switch_key"
	ActionOrganizationEncryptionConfigCompleteSwitch Action = "organization_encryption_config:complete_switch"
	ActionOrganizationEncryptionConfigRollbackSwitch Action = "organization_encryption_config:rollback_switch"
	ActionOrganizationEncryptionConfigSuspend        Action = "organization_encryption_config:suspend"
	ActionOrganizationEncryptionConfigResume         Action = "organization_encryption_config:resume"
)

// OrganizationEncryptionConfigSnapshot is the audited state of an
// organization's encryption configuration. It names keys by id and never
// carries key material.
type OrganizationEncryptionConfigSnapshot struct {
	Mode                     string `json:"mode"`
	State                    string `json:"state"`
	KekExternalKeyID         string `json:"kek_external_key_id"`
	PreviousKekExternalKeyID string `json:"previous_kek_external_key_id,omitempty"`
}

// LogOrganizationEncryptionConfigEvent records a change to which key
// encryption key protects an organization's data, or to whether it is usable.
type LogOrganizationEncryptionConfigEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ConfigURN urn.OrganizationEncryptionConfig

	ConfigSnapshotBefore *OrganizationEncryptionConfigSnapshot
	ConfigSnapshotAfter  *OrganizationEncryptionConfigSnapshot
}

func (l *Logger) LogOrganizationEncryptionConfigCreate(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEncryptionConfigEvent) error {
	return l.logOrganizationEncryptionConfig(ctx, dbtx, ActionOrganizationEncryptionConfigCreate, event)
}

func (l *Logger) LogOrganizationEncryptionConfigSwitchKey(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEncryptionConfigEvent) error {
	return l.logOrganizationEncryptionConfig(ctx, dbtx, ActionOrganizationEncryptionConfigSwitchKey, event)
}

func (l *Logger) LogOrganizationEncryptionConfigCompleteSwitch(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEncryptionConfigEvent) error {
	return l.logOrganizationEncryptionConfig(ctx, dbtx, ActionOrganizationEncryptionConfigCompleteSwitch, event)
}

func (l *Logger) LogOrganizationEncryptionConfigRollbackSwitch(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEncryptionConfigEvent) error {
	return l.logOrganizationEncryptionConfig(ctx, dbtx, ActionOrganizationEncryptionConfigRollbackSwitch, event)
}

func (l *Logger) LogOrganizationEncryptionConfigSuspend(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEncryptionConfigEvent) error {
	return l.logOrganizationEncryptionConfig(ctx, dbtx, ActionOrganizationEncryptionConfigSuspend, event)
}

func (l *Logger) LogOrganizationEncryptionConfigResume(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEncryptionConfigEvent) error {
	return l.logOrganizationEncryptionConfig(ctx, dbtx, ActionOrganizationEncryptionConfigResume, event)
}

func (l *Logger) logOrganizationEncryptionConfig(ctx context.Context, dbtx repo.DBTX, action Action, event LogOrganizationEncryptionConfigEvent) error {
	before, err := marshalAuditPayload(event.ConfigSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	after, err := marshalAuditPayload(event.ConfigSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		IsInternal:     false,
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ConfigURN.ID.String(),
		SubjectType:        string(subjectTypeOrganizationEncryptionConfig),
		SubjectDisplayName: conv.ToPGTextEmpty("Organization encryption"),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: before,
		AfterSnapshot:  after,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OrganizationEncryptionConfigV1})
}
