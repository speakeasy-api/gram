package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionKillswitchActivate   Action = "killswitch:activate"
	ActionKillswitchChange     Action = "killswitch:change"
	ActionKillswitchDeactivate Action = "killswitch:deactivate"
	ActionKillswitchExpire     Action = "killswitch:expire"
)

// KillswitchVersionSnapshot is the bounded audit projection of one committed
// prescription version. Internal notes are admin-only and deliberately absent:
// audit reads and their webhook copies are organization-visible, not
// admin-only.
type KillswitchVersionSnapshot struct {
	Version int64  `json:"version"`
	State   string `json:"state"`
}

type killswitchLifecycleMetadata struct {
	Operation   string    `json:"operation"`
	OperationID uuid.UUID `json:"operation_id"`
}

type LogKillswitchLifecycleEvent struct {
	OrganizationID string

	Actor urn.Principal

	// Action must be one of the killswitch lifecycle actions; reactivation is
	// recorded as an activation.
	Action Action

	PrescriptionURN urn.KillswitchPrescription
	Version         int64
	State           string

	// Operation is the raw lifecycle operation and OperationReceipt the durable
	// operation receipt this transition committed under.
	Operation        string
	OperationReceipt uuid.UUID
}

func (l *Logger) LogKillswitchLifecycle(ctx context.Context, dbtx repo.DBTX, event LogKillswitchLifecycleEvent) error {
	action := event.Action
	switch action {
	case ActionKillswitchActivate, ActionKillswitchChange, ActionKillswitchDeactivate:
	default:
		return fmt.Errorf("invalid killswitch lifecycle audit action %q", action)
	}

	afterSnapshot, err := marshalAuditPayload(KillswitchVersionSnapshot{Version: event.Version, State: event.State})
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}
	metadata, err := marshalAuditPayload(killswitchLifecycleMetadata{Operation: event.Operation, OperationID: event.OperationReceipt})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.ToPGTextEmpty(""),
		ActorSlug:        conv.ToPGTextEmpty(""),

		Action: string(action),

		SubjectID:          event.PrescriptionURN.ID.String(),
		SubjectType:        string(subjectTypeKillswitchPrescription),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.KillswitchV1})
}

type killswitchExpireMetadata struct {
	Version   int64     `json:"version"`
	ExpiredAt time.Time `json:"expired_at"`
}

// LogKillswitchExpireEvent records that one specific prescription version
// reached its expiry deadline while still current. Effective state is decided
// at query time from database clock and expires_at; this entry is history
// only.
type LogKillswitchExpireEvent struct {
	OrganizationID string

	Actor urn.Principal

	PrescriptionURN urn.KillswitchPrescription
	Version         int64
	ExpiredAt       time.Time
}

func (l *Logger) LogKillswitchExpire(ctx context.Context, dbtx repo.DBTX, event LogKillswitchExpireEvent) error {
	action := ActionKillswitchExpire

	metadata, err := marshalAuditPayload(killswitchExpireMetadata{Version: event.Version, ExpiredAt: event.ExpiredAt})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.ToPGTextEmpty(""),
		ActorSlug:        conv.ToPGTextEmpty(""),

		Action: string(action),

		SubjectID:          event.PrescriptionURN.ID.String(),
		SubjectType:        string(subjectTypeKillswitchPrescription),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.KillswitchV1})
}
