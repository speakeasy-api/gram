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
	ActionOktaResourceConnectionConfirm Action = "okta-resource-connection:confirm"
	ActionOktaResourceConnectionReset   Action = "okta-resource-connection:reset"
)

// OktaResourceConnectionSnapshot is the row state an audit entry records; it
// never carries raw exchange errors.
type OktaResourceConnectionSnapshot struct {
	ConnectionID      string `json:"identity_provider_connection_id"`
	IssuerID          string `json:"remote_session_issuer_id"`
	Resource          string `json:"resource"`
	Audience          string `json:"audience"`
	OktaApplicationID string `json:"okta_application_id,omitempty"`
	State             string `json:"state"`
}

type LogOktaResourceConnectionEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ResourceConnectionURN urn.OktaResourceConnection
	ServerName            string
	ServerSlug            string
	SnapshotBefore        *OktaResourceConnectionSnapshot
	SnapshotAfter         *OktaResourceConnectionSnapshot
}

func (l *Logger) LogOktaResourceConnectionConfirm(ctx context.Context, dbtx repo.DBTX, event LogOktaResourceConnectionEvent) error {
	return l.logOktaResourceConnection(ctx, dbtx, ActionOktaResourceConnectionConfirm, event)
}

func (l *Logger) LogOktaResourceConnectionReset(ctx context.Context, dbtx repo.DBTX, event LogOktaResourceConnectionEvent) error {
	return l.logOktaResourceConnection(ctx, dbtx, ActionOktaResourceConnectionReset, event)
}

func (l *Logger) logOktaResourceConnection(ctx context.Context, dbtx repo.DBTX, action Action, event LogOktaResourceConnectionEvent) error {
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
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ResourceConnectionURN.ID.String(),
		SubjectType:        string(subjectTypeOktaResourceConnection),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ServerName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ServerSlug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OktaResourceConnectionV1})
}
