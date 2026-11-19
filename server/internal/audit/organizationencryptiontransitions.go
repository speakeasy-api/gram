package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Transition events record platform administrators moving an organization's
// stored values from the platform key to its organization key. Customers do
// not choose or drive these steps; their own choice of key is audited as an
// organization encryption config event. Transition events are therefore
// internal: kept for staff, never shown in customer audit lists, and never
// published to any outbox consumer, so the subject has no outbox event.
const (
	ActionOrganizationEncryptionTransitionEnroll              Action = "organization_encryption_transition:enroll"
	ActionOrganizationEncryptionTransitionRollback            Action = "organization_encryption_transition:rollback"
	ActionOrganizationEncryptionTransitionFinalize            Action = "organization_encryption_transition:finalize"
	ActionOrganizationEncryptionTransitionStartOrgKMS         Action = "organization_encryption_transition:start_org_kms"
	ActionOrganizationEncryptionTransitionClearPlatformCopies Action = "organization_encryption_transition:clear_platform_copies"
)

// OrganizationEncryptionTransitionSnapshot is the audited state of one use
// case's move from the platform key to the organization's key.
type OrganizationEncryptionTransitionSnapshot struct {
	// UseCase is the organization encryption use case being moved.
	UseCase string `json:"use_case"`

	// State is platform_key, dual_write or org_kms.
	State string `json:"state"`

	// Generation increases on every state change.
	Generation int64 `json:"generation"`

	// PlatformCopiesCleared reports whether the platform-key copies have been
	// destroyed.
	PlatformCopiesCleared bool `json:"platform_copies_cleared"`
}

// OrganizationEncryptionTransitionMetadata records the checks a finalize or
// clearing passed. It never carries values, ciphertext or key material.
type OrganizationEncryptionTransitionMetadata struct {
	// MinimumDualWriteSeconds is the minimum time in dual_write that finalize
	// required.
	MinimumDualWriteSeconds int64 `json:"minimum_dual_write_seconds,omitempty"`

	// UnreadableValues is how many values were recorded as unreadable when
	// finalize ran. Clearing destroys their platform-key copies.
	UnreadableValues int64 `json:"unreadable_values,omitempty"`

	// ClearedValues is how many platform-key copies clearing destroyed.
	ClearedValues int64 `json:"cleared_values,omitempty"`
}

// LogOrganizationEncryptionTransitionEvent records a change to where an
// organization is in moving one use case to its own key.
type LogOrganizationEncryptionTransitionEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TransitionURN urn.OrganizationEncryptionTransition

	TransitionSnapshotBefore *OrganizationEncryptionTransitionSnapshot
	TransitionSnapshotAfter  *OrganizationEncryptionTransitionSnapshot

	Metadata *OrganizationEncryptionTransitionMetadata
}

func (l *Logger) LogOrganizationEncryptionTransitionEnroll(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEncryptionTransitionEvent) error {
	return l.logOrganizationEncryptionTransition(ctx, dbtx, ActionOrganizationEncryptionTransitionEnroll, event)
}

func (l *Logger) LogOrganizationEncryptionTransitionRollback(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEncryptionTransitionEvent) error {
	return l.logOrganizationEncryptionTransition(ctx, dbtx, ActionOrganizationEncryptionTransitionRollback, event)
}

func (l *Logger) LogOrganizationEncryptionTransitionFinalize(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEncryptionTransitionEvent) error {
	return l.logOrganizationEncryptionTransition(ctx, dbtx, ActionOrganizationEncryptionTransitionFinalize, event)
}

func (l *Logger) LogOrganizationEncryptionTransitionStartOrgKMS(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEncryptionTransitionEvent) error {
	return l.logOrganizationEncryptionTransition(ctx, dbtx, ActionOrganizationEncryptionTransitionStartOrgKMS, event)
}

func (l *Logger) LogOrganizationEncryptionTransitionClearPlatformCopies(ctx context.Context, dbtx repo.DBTX, event LogOrganizationEncryptionTransitionEvent) error {
	return l.logOrganizationEncryptionTransition(ctx, dbtx, ActionOrganizationEncryptionTransitionClearPlatformCopies, event)
}

func (l *Logger) logOrganizationEncryptionTransition(ctx context.Context, dbtx repo.DBTX, action Action, event LogOrganizationEncryptionTransitionEvent) error {
	before, err := marshalAuditPayload(event.TransitionSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	after, err := marshalAuditPayload(event.TransitionSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	metadata, err := marshalAuditPayload(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	useCase := ""
	switch {
	case event.TransitionSnapshotAfter != nil:
		useCase = event.TransitionSnapshotAfter.UseCase
	case event.TransitionSnapshotBefore != nil:
		useCase = event.TransitionSnapshotBefore.UseCase
	}

	entry := repo.InsertAuditLogParams{
		IsInternal:     true,
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.TransitionURN.ID.String(),
		SubjectType:        string(subjectTypeOrganizationEncryptionTransition),
		SubjectDisplayName: conv.ToPGTextEmpty("Organization encryption: " + useCase),
		SubjectSlug:        conv.ToPGTextEmpty(useCase),

		Metadata:       metadata,
		BeforeSnapshot: before,
		AfterSnapshot:  after,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: nil})
}
