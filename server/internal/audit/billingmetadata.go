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
	ActionBillingMetadataUpdate Action = "billing_metadata:update"
)

// BillingMetadataSnapshot captures an organization's billing contract terms
// for audit before/after snapshots.
type BillingMetadataSnapshot struct {
	TumMonthlyTokenLimit    *int64  `json:"tum_monthly_token_limit"`
	TunnelledMcpServerLimit *int    `json:"tunnelled_mcp_server_limit"`
	AlertEmail              *string `json:"alert_email"`
	BillingCycleAnchorDay   int     `json:"billing_cycle_anchor_day"`
}

type LogBillingMetadataUpdateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	BillingMetadataURN urn.BillingMetadata

	BillingMetadataSnapshotBefore *BillingMetadataSnapshot
	BillingMetadataSnapshotAfter  *BillingMetadataSnapshot
}

func (l *Logger) LogBillingMetadataUpdate(ctx context.Context, dbtx repo.DBTX, event LogBillingMetadataUpdateEvent) error {
	action := ActionBillingMetadataUpdate

	beforeSnapshot, err := marshalAuditPayload(event.BillingMetadataSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.BillingMetadataSnapshotAfter)
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

		SubjectID:          event.BillingMetadataURN.ID.String(),
		SubjectType:        string(subjectTypeBillingMetadata),
		SubjectDisplayName: conv.ToPGTextEmpty("Billing metadata"),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.BillingMetadataV1})
}
