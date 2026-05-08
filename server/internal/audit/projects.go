package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionProjectCreate Action = "project:create"
	ActionProjectUpdate Action = "project:update"
	ActionProjectDelete Action = "project:delete"
)

type LogProjectCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ProjectName string
	ProjectSlug string
}

func (l *Logger) LogProjectCreate(ctx context.Context, dbtx repo.DBTX, event LogProjectCreateEvent) error {
	action := ActionProjectCreate

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ProjectID.String(),
		SubjectType:        "project",
		SubjectDisplayName: conv.ToPGTextEmpty(event.ProjectName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ProjectSlug),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}
	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogProjectUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ProjectName string
	ProjectSlug string

	ProjectSnapshotBefore *gen.Project
	ProjectSnapshotAfter  *gen.Project
}

func (l *Logger) LogProjectUpdate(ctx context.Context, dbtx repo.DBTX, event LogProjectUpdateEvent) error {
	action := ActionProjectUpdate

	beforeSnapshot, err := marshalAuditPayload(event.ProjectSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.ProjectSnapshotAfter)
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

		SubjectID:          event.ProjectID.String(),
		SubjectType:        string(subjectTypeProject),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ProjectName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ProjectSlug),

		Metadata:       nil,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}
	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogProjectDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ProjectName string
	ProjectSlug string
}

func (l *Logger) LogProjectDelete(ctx context.Context, dbtx repo.DBTX, event LogProjectDeleteEvent) error {
	action := ActionProjectDelete

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ProjectID.String(),
		SubjectType:        string(subjectTypeProject),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ProjectName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ProjectSlug),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}
	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
