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
	ActionJsonWebKeyPublish  Action = "json_web_key:publish"
	ActionJsonWebKeyActivate Action = "json_web_key:activate"
	ActionJsonWebKeyRetire   Action = "json_web_key:retire"
	ActionJsonWebKeyRevoke   Action = "json_web_key:revoke"
	ActionJsonWebKeyDelete   Action = "json_web_key:delete"
)

// JsonWebKeySnapshot is the audited state of a published JSON Web Key. The
// public_jwk document is deliberately absent: it is derivable from the kid (an
// RFC 7638 thumbprint of the key material) and never changes after publication,
// so recording it on every transition would bloat entries without evidencing
// anything the kid does not.
type JsonWebKeySnapshot struct {
	Kid           string `json:"kid"`
	State         string `json:"state"`
	ExternalKeyID string `json:"external_key_id"`
}

// JsonWebKeyAuditMetadata associates a key event with the set it belongs to,
// since the audit subject only identifies the key row itself.
type JsonWebKeyAuditMetadata struct {
	JsonWebKeySetID string `json:"json_web_key_set_id"`
}

// LogJsonWebKeyEvent is the shared payload for every json_web_key action. The
// key's kid doubles as the subject display name. Snapshots are populated on the
// lifecycle transitions (activate, retire, revoke) and left nil on publish and
// delete, matching the create/delete convention elsewhere.
type LogJsonWebKeyEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	KeyURN urn.JsonWebKey
	Kid    string
	SetURN urn.JsonWebKeySet

	KeySnapshotBefore *JsonWebKeySnapshot
	KeySnapshotAfter  *JsonWebKeySnapshot
}

func (l *Logger) LogJsonWebKeyPublish(ctx context.Context, dbtx repo.DBTX, event LogJsonWebKeyEvent) error {
	return l.logJsonWebKey(ctx, dbtx, ActionJsonWebKeyPublish, event)
}

func (l *Logger) LogJsonWebKeyActivate(ctx context.Context, dbtx repo.DBTX, event LogJsonWebKeyEvent) error {
	return l.logJsonWebKey(ctx, dbtx, ActionJsonWebKeyActivate, event)
}

func (l *Logger) LogJsonWebKeyRetire(ctx context.Context, dbtx repo.DBTX, event LogJsonWebKeyEvent) error {
	return l.logJsonWebKey(ctx, dbtx, ActionJsonWebKeyRetire, event)
}

func (l *Logger) LogJsonWebKeyRevoke(ctx context.Context, dbtx repo.DBTX, event LogJsonWebKeyEvent) error {
	return l.logJsonWebKey(ctx, dbtx, ActionJsonWebKeyRevoke, event)
}

func (l *Logger) LogJsonWebKeyDelete(ctx context.Context, dbtx repo.DBTX, event LogJsonWebKeyEvent) error {
	return l.logJsonWebKey(ctx, dbtx, ActionJsonWebKeyDelete, event)
}

// logJsonWebKey builds the entry every json_web_key action shares. The five
// actions differ only in the action string — one payload struct serves them
// all, unlike subjects whose update event carries fields the others lack.
func (l *Logger) logJsonWebKey(ctx context.Context, dbtx repo.DBTX, action Action, event LogJsonWebKeyEvent) error {
	metadata, err := marshalAuditPayload(JsonWebKeyAuditMetadata{JsonWebKeySetID: event.SetURN.ID.String()})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	before, err := marshalAuditPayload(event.KeySnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	after, err := marshalAuditPayload(event.KeySnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.KeyURN.ID.String(),
		SubjectType:        string(subjectTypeJsonWebKey),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Kid),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       metadata,
		BeforeSnapshot: before,
		AfterSnapshot:  after,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.JsonWebKeyV1})
}
