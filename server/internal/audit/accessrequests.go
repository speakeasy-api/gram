package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionAccessRequestCreate   Action = "access_request:create"
	ActionAccessRequestApprove  Action = "access_request:approve"
	ActionAccessRequestDeny     Action = "access_request:deny"
	ActionAccessRequestWithdraw Action = "access_request:withdraw"
)

// accessRequestAuditMetadata is recorded on every access request event so the
// log answers who asked for what and how it was decided without decoding the
// snapshots.
type accessRequestAuditMetadata struct {
	RequesterUserID  string `json:"requester_user_id"`
	Scope            string `json:"scope"`
	DecidedVia       string `json:"decided_via,omitempty"`
	TemporaryGrantID string `json:"temporary_grant_id,omitempty"`
}

type LogAccessRequestCreateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AccessRequestURN urn.AccessRequest

	AccessRequestSnapshotAfter *accessgen.AccessRequest
}

func (l *Logger) LogAccessRequestCreate(ctx context.Context, dbtx repo.DBTX, event LogAccessRequestCreateEvent) error {
	return l.logAccessRequest(ctx, dbtx, ActionAccessRequestCreate, event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.AccessRequestURN, nil, event.AccessRequestSnapshotAfter)
}

// LogAccessRequestDecideEvent records an approval or a denial. An approval
// also writes the temporary grant's own access_grant:create_temporary event.
type LogAccessRequestDecideEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AccessRequestURN urn.AccessRequest

	AccessRequestSnapshotBefore *accessgen.AccessRequest
	AccessRequestSnapshotAfter  *accessgen.AccessRequest
}

func (l *Logger) LogAccessRequestApprove(ctx context.Context, dbtx repo.DBTX, event LogAccessRequestDecideEvent) error {
	return l.logAccessRequest(ctx, dbtx, ActionAccessRequestApprove, event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.AccessRequestURN, event.AccessRequestSnapshotBefore, event.AccessRequestSnapshotAfter)
}

func (l *Logger) LogAccessRequestDeny(ctx context.Context, dbtx repo.DBTX, event LogAccessRequestDecideEvent) error {
	return l.logAccessRequest(ctx, dbtx, ActionAccessRequestDeny, event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.AccessRequestURN, event.AccessRequestSnapshotBefore, event.AccessRequestSnapshotAfter)
}

type LogAccessRequestWithdrawEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AccessRequestURN urn.AccessRequest

	AccessRequestSnapshotBefore *accessgen.AccessRequest
	AccessRequestSnapshotAfter  *accessgen.AccessRequest
}

func (l *Logger) LogAccessRequestWithdraw(ctx context.Context, dbtx repo.DBTX, event LogAccessRequestWithdrawEvent) error {
	return l.logAccessRequest(ctx, dbtx, ActionAccessRequestWithdraw, event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.AccessRequestURN, event.AccessRequestSnapshotBefore, event.AccessRequestSnapshotAfter)
}

func (l *Logger) logAccessRequest(
	ctx context.Context,
	dbtx repo.DBTX,
	action Action,
	organizationID string,
	actor urn.Principal,
	actorDisplayName *string,
	actorSlug *string,
	requestURN urn.AccessRequest,
	before *accessgen.AccessRequest,
	after *accessgen.AccessRequest,
) error {
	if after == nil {
		return fmt.Errorf("%s: missing access request snapshot", action)
	}
	beforeSnapshot, err := marshalAuditPayload(before)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(after)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}
	metadata, err := marshalAuditPayload(accessRequestAuditMetadata{
		RequesterUserID:  after.RequesterUserID,
		Scope:            after.Scope,
		DecidedVia:       conv.PtrValOr(after.DecidedVia, ""),
		TemporaryGrantID: conv.PtrValOr(after.TemporaryGrantID, ""),
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		IsInternal:     false,
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          actor.ID,
		ActorType:        string(actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(actorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(actorSlug),

		Action: string(action),

		SubjectID:          requestURN.ID.String(),
		SubjectType:        string(subjectTypeAccessRequest),
		SubjectDisplayName: conv.ToPGTextEmpty(after.Scope + " for " + after.RequesterName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AccessRequestV1})
}
