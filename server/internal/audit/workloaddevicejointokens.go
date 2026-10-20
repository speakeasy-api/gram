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
	// Creating a join token hands out the right to enroll one machine under an
	// agent, redeeming it is that enrollment, and revoking it withdraws that
	// right before any machine used it.
	ActionWorkloadDeviceJoinTokenCreate Action = "workload-device-join-token:create"
	ActionWorkloadDeviceJoinTokenRedeem Action = "workload-device-join-token:redeem"
	ActionWorkloadDeviceJoinTokenRevoke Action = "workload-device-join-token:revoke"
)

// WorkloadDeviceJoinTokenSnapshot is the audited state of one join token. The
// token value is never recorded.
type WorkloadDeviceJoinTokenSnapshot struct {
	AgentID   string `json:"agent_id"`
	ExpiresAt string `json:"expires_at"`

	// DeviceSubject is the device the token enrolled; empty until redeemed.
	DeviceSubject string `json:"device_subject"`
	// Hostname is what the device reported at enrollment; empty when none.
	Hostname string `json:"hostname"`
}

type LogWorkloadDeviceJoinTokenCreateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	JoinTokenURN urn.WorkloadDeviceJoinToken
	// The agent's name: a token has no label of its own, and the agent is what
	// it grants.
	JoinTokenDisplayName string

	JoinTokenSnapshotAfter *WorkloadDeviceJoinTokenSnapshot
}

func (l *Logger) LogWorkloadDeviceJoinTokenCreate(ctx context.Context, dbtx repo.DBTX, event LogWorkloadDeviceJoinTokenCreateEvent) error {
	action := ActionWorkloadDeviceJoinTokenCreate

	after, err := marshalAuditPayload(event.JoinTokenSnapshotAfter)
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

		SubjectID:          event.JoinTokenURN.ID.String(),
		SubjectType:        string(subjectTypeWorkloadDeviceJoinToken),
		SubjectDisplayName: conv.ToPGTextEmpty(event.JoinTokenDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  after,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.WorkloadDeviceJoinTokenV1})
}

type LogWorkloadDeviceJoinTokenRedeemEvent struct {
	OrganizationID string

	// The administrator who created the token: redemption carries no session,
	// and the token is that person's authorization of the enrollment.
	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	JoinTokenURN         urn.WorkloadDeviceJoinToken
	JoinTokenDisplayName string

	// Redeeming spends an existing token, so the trail records it unspent and
	// spent: what was redeemed, and by which device.
	JoinTokenSnapshotBefore *WorkloadDeviceJoinTokenSnapshot
	JoinTokenSnapshotAfter  *WorkloadDeviceJoinTokenSnapshot
}

func (l *Logger) LogWorkloadDeviceJoinTokenRedeem(ctx context.Context, dbtx repo.DBTX, event LogWorkloadDeviceJoinTokenRedeemEvent) error {
	action := ActionWorkloadDeviceJoinTokenRedeem

	before, err := marshalAuditPayload(event.JoinTokenSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	after, err := marshalAuditPayload(event.JoinTokenSnapshotAfter)
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

		SubjectID:          event.JoinTokenURN.ID.String(),
		SubjectType:        string(subjectTypeWorkloadDeviceJoinToken),
		SubjectDisplayName: conv.ToPGTextEmpty(event.JoinTokenDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: before,
		AfterSnapshot:  after,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.WorkloadDeviceJoinTokenV1})
}

type LogWorkloadDeviceJoinTokenRevokeEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	JoinTokenURN         urn.WorkloadDeviceJoinToken
	JoinTokenDisplayName string

	// The unspent token as it stood when revoked. A revoked token has no
	// state after, as with any delete.
	JoinTokenSnapshotBefore *WorkloadDeviceJoinTokenSnapshot
}

func (l *Logger) LogWorkloadDeviceJoinTokenRevoke(ctx context.Context, dbtx repo.DBTX, event LogWorkloadDeviceJoinTokenRevokeEvent) error {
	action := ActionWorkloadDeviceJoinTokenRevoke

	before, err := marshalAuditPayload(event.JoinTokenSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.JoinTokenURN.ID.String(),
		SubjectType:        string(subjectTypeWorkloadDeviceJoinToken),
		SubjectDisplayName: conv.ToPGTextEmpty(event.JoinTokenDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: before,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.WorkloadDeviceJoinTokenV1})
}
