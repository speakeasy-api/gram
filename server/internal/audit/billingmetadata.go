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
	ActionBillingMetadataCancelStripeSubscription Action = "billing_metadata:cancel_stripe_subscription"
	ActionBillingMetadataCreateStripeCheckout     Action = "billing_metadata:create_stripe_checkout"
	ActionBillingMetadataCreateStripePortal       Action = "billing_metadata:create_stripe_portal"
	ActionBillingMetadataResumeStripeSubscription Action = "billing_metadata:resume_stripe_subscription"
	ActionBillingMetadataUpdate                   Action = "billing_metadata:update"
)

// BillingMetadataSnapshot captures an organization's billing contract terms
// for audit before/after snapshots.
type BillingMetadataSnapshot struct {
	TumMonthlyTokenLimit   *int64  `json:"tum_monthly_token_limit"`
	TunneledMcpServerLimit *int    `json:"tunneled_mcp_server_limit"`
	AlertEmail             *string `json:"alert_email"`
	BillingCycleAnchorDay  int     `json:"billing_cycle_anchor_day"`
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

type LogBillingMetadataCreateStripeCheckoutEvent struct {
	// OrganizationID is the organization starting PAYG billing.
	OrganizationID string

	// Actor is the user who requested the Checkout session.
	Actor urn.Principal

	// ActorDisplayName is the user's display label.
	ActorDisplayName *string

	// ActorSlug is the user's audit-log slug, when available.
	ActorSlug *string

	// BillingMetadataURN identifies the organization billing metadata subject.
	BillingMetadataURN urn.BillingMetadata
}

type LogBillingMetadataCreateStripePortalEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	BillingMetadataURN urn.BillingMetadata
}

type LogBillingMetadataCancelStripeSubscriptionEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	BillingMetadataURN urn.BillingMetadata
}

type LogBillingMetadataResumeStripeSubscriptionEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	BillingMetadataURN urn.BillingMetadata
}

func (l *Logger) LogBillingMetadataCreateStripeCheckout(ctx context.Context, dbtx repo.DBTX, event LogBillingMetadataCreateStripeCheckoutEvent) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(ActionBillingMetadataCreateStripeCheckout),

		SubjectID:          event.BillingMetadataURN.ID.String(),
		SubjectType:        string(subjectTypeBillingMetadata),
		SubjectDisplayName: conv.ToPGTextEmpty("Billing metadata"),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.BillingMetadataV1})
}

func (l *Logger) LogBillingMetadataCreateStripePortal(ctx context.Context, dbtx repo.DBTX, event LogBillingMetadataCreateStripePortalEvent) error {
	return l.logBillingMetadataAction(ctx, dbtx, event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.BillingMetadataURN, ActionBillingMetadataCreateStripePortal)
}

func (l *Logger) LogBillingMetadataCancelStripeSubscription(ctx context.Context, dbtx repo.DBTX, event LogBillingMetadataCancelStripeSubscriptionEvent) error {
	return l.logBillingMetadataAction(ctx, dbtx, event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.BillingMetadataURN, ActionBillingMetadataCancelStripeSubscription)
}

func (l *Logger) LogBillingMetadataResumeStripeSubscription(ctx context.Context, dbtx repo.DBTX, event LogBillingMetadataResumeStripeSubscriptionEvent) error {
	return l.logBillingMetadataAction(ctx, dbtx, event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.BillingMetadataURN, ActionBillingMetadataResumeStripeSubscription)
}

func (l *Logger) logBillingMetadataAction(
	ctx context.Context,
	dbtx repo.DBTX,
	organizationID string,
	actor urn.Principal,
	actorDisplayName *string,
	actorSlug *string,
	billingMetadataURN urn.BillingMetadata,
	action Action,
) error {
	entry := repo.InsertAuditLogParams{
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          actor.ID,
		ActorType:        string(actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(actorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(actorSlug),

		Action: string(action),

		SubjectID:          billingMetadataURN.ID.String(),
		SubjectType:        string(subjectTypeBillingMetadata),
		SubjectDisplayName: conv.ToPGTextEmpty("Billing metadata"),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.BillingMetadataV1})
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
