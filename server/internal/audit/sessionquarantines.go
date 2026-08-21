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
	ActionSessionQuarantineOpen    Action = "session_quarantine:open"
	ActionSessionQuarantineRelease Action = "session_quarantine:release"
)

type SessionQuarantineMetadata struct {
	SessionID      string `json:"session_id"`
	RiskPolicyID   string `json:"risk_policy_id,omitempty"`
	RiskPolicyName string `json:"risk_policy_name"`
	UserID         string `json:"user_id,omitempty"`
	Reason         string `json:"reason"`
}

type LogSessionQuarantineEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SessionQuarantineURN urn.SessionQuarantine
	RiskPolicyName       string
	Metadata             SessionQuarantineMetadata
}

func (l *Logger) LogSessionQuarantineOpen(ctx context.Context, dbtx repo.DBTX, event LogSessionQuarantineEvent) error {
	return l.logSessionQuarantine(ctx, dbtx, ActionSessionQuarantineOpen, event)
}

func (l *Logger) LogSessionQuarantineRelease(ctx context.Context, dbtx repo.DBTX, event LogSessionQuarantineEvent) error {
	return l.logSessionQuarantine(ctx, dbtx, ActionSessionQuarantineRelease, event)
}

func (l *Logger) logSessionQuarantine(ctx context.Context, dbtx repo.DBTX, action Action, event LogSessionQuarantineEvent) error {
	metadata, err := marshalAuditPayload(event.Metadata)
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.SessionQuarantineURN.ID.String(),
		SubjectType:        string(subjectTypeSessionQuarantine),
		SubjectDisplayName: conv.ToPGTextEmpty(event.RiskPolicyName),
		SubjectSlug:        conv.ToPGTextEmpty(event.Metadata.SessionID),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RiskPolicyV1})
}
