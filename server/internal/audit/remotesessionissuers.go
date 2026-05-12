package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionRemoteSessionIssuerCreate Action = "remote-session-issuer:create"
	ActionRemoteSessionIssuerUpdate Action = "remote-session-issuer:update"
	ActionRemoteSessionIssuerDelete Action = "remote-session-issuer:delete"
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
		SubjectDisplayName: conv.ToPGTextEmpty(event.IssuerURL),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, entry)
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
		SubjectDisplayName: conv.ToPGTextEmpty(event.IssuerURL),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, entry)
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
		SubjectDisplayName: conv.ToPGTextEmpty(event.IssuerURL),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, entry)
}
