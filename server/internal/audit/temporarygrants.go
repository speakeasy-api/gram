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
	ActionAccessGrantCreateTemporary Action = "access_grant:create_temporary"
	ActionAccessGrantRenewTemporary  Action = "access_grant:renew_temporary"
	ActionAccessGrantRevokeTemporary Action = "access_grant:revoke_temporary"
)

// temporaryGrantAuditMetadata is recorded on every temporary grant event so
// the log answers who received what, until when, why, and whether the
// recipient acted on their own access, without decoding the snapshots. The
// grant row itself keeps none of this: the audit log is its record.
type temporaryGrantAuditMetadata struct {
	RecipientURN string `json:"recipient_urn"`
	Scope        string `json:"scope"`
	ExpiresAt    string `json:"expires_at"`
	Reason       string `json:"reason,omitempty"`
	// SelfGranted marks an event whose audited actor is the grant's
	// recipient. Creating and renewing take org:admin, so on those events it
	// means an administrator gave or extended access to themselves (for
	// example mcp:connect). On revoke it also covers a member ending a grant
	// given directly to them.
	SelfGranted bool `json:"self_granted"`
}

func temporaryGrantMetadata(actor urn.Principal, grant *accessgen.TemporaryGrant, reason string) ([]byte, error) {
	return marshalAuditPayload(temporaryGrantAuditMetadata{
		RecipientURN: grant.PrincipalUrn,
		Scope:        grant.Scope,
		ExpiresAt:    grant.ExpiresAt,
		Reason:       reason,
		SelfGranted:  actor.String() == grant.PrincipalUrn,
	})
}

type LogAccessTemporaryGrantCreateEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AccessGrantURN urn.AccessGrant
	RecipientEmail string
	Reason         string

	TemporaryGrantSnapshotAfter *accessgen.TemporaryGrant
}

func (l *Logger) LogAccessTemporaryGrantCreate(ctx context.Context, dbtx repo.DBTX, event LogAccessTemporaryGrantCreateEvent) error {
	action := ActionAccessGrantCreateTemporary

	afterSnapshot, err := marshalAuditPayload(event.TemporaryGrantSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}
	metadata, err := temporaryGrantMetadata(event.Actor, event.TemporaryGrantSnapshotAfter, event.Reason)
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	return l.log(ctx, dbtx, auditEntry{Params: temporaryGrantAuditParams(action, event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.AccessGrantURN, event.TemporaryGrantSnapshotAfter, event.RecipientEmail, nil, afterSnapshot, metadata), OutboxEvent: events.AccessGrantV1})
}

type LogAccessTemporaryGrantRenewEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AccessGrantURN urn.AccessGrant
	RecipientEmail string

	TemporaryGrantSnapshotBefore *accessgen.TemporaryGrant
	TemporaryGrantSnapshotAfter  *accessgen.TemporaryGrant
}

func (l *Logger) LogAccessTemporaryGrantRenew(ctx context.Context, dbtx repo.DBTX, event LogAccessTemporaryGrantRenewEvent) error {
	action := ActionAccessGrantRenewTemporary

	beforeSnapshot, err := marshalAuditPayload(event.TemporaryGrantSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.TemporaryGrantSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}
	metadata, err := temporaryGrantMetadata(event.Actor, event.TemporaryGrantSnapshotAfter, "")
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	return l.log(ctx, dbtx, auditEntry{Params: temporaryGrantAuditParams(action, event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.AccessGrantURN, event.TemporaryGrantSnapshotAfter, event.RecipientEmail, beforeSnapshot, afterSnapshot, metadata), OutboxEvent: events.AccessGrantV1})
}

type LogAccessTemporaryGrantRevokeEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AccessGrantURN urn.AccessGrant
	RecipientEmail string

	TemporaryGrantSnapshotBefore *accessgen.TemporaryGrant
	TemporaryGrantSnapshotAfter  *accessgen.TemporaryGrant
}

func (l *Logger) LogAccessTemporaryGrantRevoke(ctx context.Context, dbtx repo.DBTX, event LogAccessTemporaryGrantRevokeEvent) error {
	action := ActionAccessGrantRevokeTemporary

	beforeSnapshot, err := marshalAuditPayload(event.TemporaryGrantSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.TemporaryGrantSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}
	metadata, err := temporaryGrantMetadata(event.Actor, event.TemporaryGrantSnapshotAfter, "")
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	return l.log(ctx, dbtx, auditEntry{Params: temporaryGrantAuditParams(action, event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.AccessGrantURN, event.TemporaryGrantSnapshotAfter, event.RecipientEmail, beforeSnapshot, afterSnapshot, metadata), OutboxEvent: events.AccessGrantV1})
}

func temporaryGrantAuditParams(
	action Action,
	organizationID string,
	actor urn.Principal,
	actorDisplayName *string,
	actorSlug *string,
	grantURN urn.AccessGrant,
	grant *accessgen.TemporaryGrant,
	recipientEmail string,
	beforeSnapshot []byte,
	afterSnapshot []byte,
	metadata []byte,
) repo.InsertAuditLogParams {
	// A member is named by email; any other principal (a role, an agent,
	// everyone) by its URN.
	recipient := recipientEmail
	if recipient == "" {
		recipient = grant.PrincipalUrn
	}
	displayName := grant.Scope + " for " + recipient

	return repo.InsertAuditLogParams{
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          actor.ID,
		ActorType:        string(actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(actorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(actorSlug),

		Action: string(action),

		SubjectID:          grantURN.ID.String(),
		SubjectType:        string(subjectTypeAccessGrant),
		SubjectDisplayName: conv.ToPGTextEmpty(displayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}
}
