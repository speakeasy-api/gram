package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionUserSessionIssuerCreate  Action = "user-session-issuer:create"
	ActionUserSessionIssuerUpdate  Action = "user-session-issuer:update"
	ActionUserSessionIssuerDelete  Action = "user-session-issuer:delete"
	ActionUserSessionIssuerMigrate Action = "user-session-issuer:migrate"
)

type LogUserSessionIssuerCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	UserSessionIssuerURN           urn.UserSessionIssuer
	Slug                           string
	UserSessionIssuerSnapshotAfter *types.UserSessionIssuer
}

func (l *Logger) LogUserSessionIssuerCreate(ctx context.Context, dbtx repo.DBTX, event LogUserSessionIssuerCreateEvent) error {
	action := ActionUserSessionIssuerCreate
	afterSnapshot, err := marshalAuditPayload(event.UserSessionIssuerSnapshotAfter)
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

		SubjectID:          event.UserSessionIssuerURN.ID.String(),
		SubjectType:        string(subjectTypeUserSessionIssuer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Slug),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.UserSessionIssuerV1})
}

type LogUserSessionIssuerUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	UserSessionIssuerURN            urn.UserSessionIssuer
	Slug                            string
	UserSessionIssuerSnapshotBefore *types.UserSessionIssuer
	UserSessionIssuerSnapshotAfter  *types.UserSessionIssuer
}

func (l *Logger) LogUserSessionIssuerUpdate(ctx context.Context, dbtx repo.DBTX, event LogUserSessionIssuerUpdateEvent) error {
	action := ActionUserSessionIssuerUpdate

	beforeSnapshot, err := marshalAuditPayload(event.UserSessionIssuerSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.UserSessionIssuerSnapshotAfter)
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

		SubjectID:          event.UserSessionIssuerURN.ID.String(),
		SubjectType:        string(subjectTypeUserSessionIssuer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Slug),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.UserSessionIssuerV1})
}

// LogUserSessionIssuerMigrateEvent records the source issuer as the subject
// because it is the row that is retired by the consolidation.
type LogUserSessionIssuerMigrateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	SourceUserSessionIssuerURN urn.UserSessionIssuer
	SourceSlug                 string
	TargetUserSessionIssuerURN urn.UserSessionIssuer
	TargetSlug                 string

	ClientsMigrated        int64
	SessionsMigrated       int64
	ConsentsMigrated       int64
	CimdClientsMigrated    int64
	RemoteSessionsMigrated int64

	UserSessionIssuerSnapshotBefore *types.UserSessionIssuer
	UserSessionIssuerSnapshotAfter  *types.UserSessionIssuer
}

func (l *Logger) LogUserSessionIssuerMigrate(ctx context.Context, dbtx repo.DBTX, event LogUserSessionIssuerMigrateEvent) error {
	action := ActionUserSessionIssuerMigrate
	metadata, err := marshalAuditPayload(map[string]any{
		"source_user_session_issuer_urn": event.SourceUserSessionIssuerURN.String(),
		"target_user_session_issuer_urn": event.TargetUserSessionIssuerURN.String(),
		"target_slug":                    event.TargetSlug,
		"clients_migrated":               event.ClientsMigrated,
		"sessions_migrated":              event.SessionsMigrated,
		"consents_migrated":              event.ConsentsMigrated,
		"cimd_clients_migrated":          event.CimdClientsMigrated,
		"remote_sessions_migrated":       event.RemoteSessionsMigrated,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	beforeSnapshot, err := marshalAuditPayload(event.UserSessionIssuerSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.UserSessionIssuerSnapshotAfter)
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

		SubjectID:          event.SourceUserSessionIssuerURN.ID.String(),
		SubjectType:        string(subjectTypeUserSessionIssuer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SourceSlug),
		SubjectSlug:        conv.ToPGTextEmpty(event.SourceSlug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.UserSessionIssuerV1})
}

type LogUserSessionIssuerDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	UserSessionIssuerURN urn.UserSessionIssuer
	Slug                 string
}

func (l *Logger) LogUserSessionIssuerDelete(ctx context.Context, dbtx repo.DBTX, event LogUserSessionIssuerDeleteEvent) error {
	action := ActionUserSessionIssuerDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.UserSessionIssuerURN.ID.String(),
		SubjectType:        string(subjectTypeUserSessionIssuer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Slug),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.UserSessionIssuerV1})
}
