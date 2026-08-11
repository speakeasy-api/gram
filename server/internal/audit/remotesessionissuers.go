package audit

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionRemoteSessionIssuerCreate  Action = "remote-session-issuer:create"
	ActionRemoteSessionIssuerUpdate  Action = "remote-session-issuer:update"
	ActionRemoteSessionIssuerDelete  Action = "remote-session-issuer:delete"
	ActionRemoteSessionIssuerMigrate Action = "remote-session-issuer:migrate"
)

type LogRemoteSessionIssuerCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RemoteSessionIssuerURN urn.RemoteSessionIssuer
	Slug                   string
	IssuerURL              string
	Name                   *string
}

func (l *Logger) LogRemoteSessionIssuerCreate(ctx context.Context, dbtx repo.DBTX, event LogRemoteSessionIssuerCreateEvent) error {
	action := ActionRemoteSessionIssuerCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RemoteSessionIssuerURN.ID.String(),
		SubjectType:        string(subjectTypeRemoteSessionIssuer),
		SubjectDisplayName: conv.ToPGTextEmpty(remoteSessionIssuerDisplayName(event.Name, event.IssuerURL)),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RemoteSessionIssuerV1})
}

// remoteSessionIssuerDisplayName picks the human-facing subject label for an
// issuer audit entry: the optional display name when set, otherwise the issuer
// URL. A whitespace-only name falls back to the issuer URL.
func remoteSessionIssuerDisplayName(name *string, issuerURL string) string {
	if name != nil {
		if trimmed := strings.TrimSpace(*name); trimmed != "" {
			return trimmed
		}
	}
	return issuerURL
}

type LogRemoteSessionIssuerUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RemoteSessionIssuerURN urn.RemoteSessionIssuer
	Slug                   string
	IssuerURL              string
	Name                   *string

	SnapshotBefore *types.RemoteSessionIssuer
	SnapshotAfter  *types.RemoteSessionIssuer
}

func (l *Logger) LogRemoteSessionIssuerUpdate(ctx context.Context, dbtx repo.DBTX, event LogRemoteSessionIssuerUpdateEvent) error {
	action := ActionRemoteSessionIssuerUpdate

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

		SubjectID:          event.RemoteSessionIssuerURN.ID.String(),
		SubjectType:        string(subjectTypeRemoteSessionIssuer),
		SubjectDisplayName: conv.ToPGTextEmpty(remoteSessionIssuerDisplayName(event.Name, event.IssuerURL)),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RemoteSessionIssuerV1})
}

// LogRemoteSessionIssuerMigrateEvent records the consolidation of a source
// issuer onto a target issuer. The subject is the source: it is the row that
// disappears, and the migrate action stands in for the soft-delete that the
// same transaction performs, so no separate delete event is emitted.
type LogRemoteSessionIssuerMigrateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SourceRemoteSessionIssuerURN urn.RemoteSessionIssuer
	SourceSlug                   string
	SourceIssuerURL              string
	SourceName                   *string

	TargetRemoteSessionIssuerURN urn.RemoteSessionIssuer
	TargetSlug                   string

	ClientsMigrated int64

	SnapshotBefore *types.RemoteSessionIssuer
	SnapshotAfter  *types.RemoteSessionIssuer
}

func (l *Logger) LogRemoteSessionIssuerMigrate(ctx context.Context, dbtx repo.DBTX, event LogRemoteSessionIssuerMigrateEvent) error {
	action := ActionRemoteSessionIssuerMigrate

	metadata, err := marshalAuditPayload(map[string]any{
		"source_remote_session_issuer_urn": event.SourceRemoteSessionIssuerURN.String(),
		"target_remote_session_issuer_urn": event.TargetRemoteSessionIssuerURN.String(),
		"target_slug":                      event.TargetSlug,
		"clients_migrated":                 event.ClientsMigrated,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	// The snapshots describe the source issuer before the migration and the
	// target issuer that absorbed its clients, so a reader can see both ends of
	// the consolidation from one entry.
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

		SubjectID:          event.SourceRemoteSessionIssuerURN.ID.String(),
		SubjectType:        string(subjectTypeRemoteSessionIssuer),
		SubjectDisplayName: conv.ToPGTextEmpty(remoteSessionIssuerDisplayName(event.SourceName, event.SourceIssuerURL)),
		SubjectSlug:        conv.ToPGTextEmpty(event.SourceSlug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RemoteSessionIssuerV1})
}

type LogRemoteSessionIssuerDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RemoteSessionIssuerURN urn.RemoteSessionIssuer
	Slug                   string
	IssuerURL              string
	Name                   *string
}

func (l *Logger) LogRemoteSessionIssuerDelete(ctx context.Context, dbtx repo.DBTX, event LogRemoteSessionIssuerDeleteEvent) error {
	action := ActionRemoteSessionIssuerDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RemoteSessionIssuerURN.ID.String(),
		SubjectType:        string(subjectTypeRemoteSessionIssuer),
		SubjectDisplayName: conv.ToPGTextEmpty(remoteSessionIssuerDisplayName(event.Name, event.IssuerURL)),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.RemoteSessionIssuerV1})
}
