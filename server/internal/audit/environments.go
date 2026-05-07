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
	ActionEnvironmentCreate Action = "environment:create"
	ActionEnvironmentUpdate Action = "environment:update"
	ActionEnvironmentDelete Action = "environment:delete"
)

type LogEnvironmentCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	EnvironmentURN  urn.Environment
	EnvironmentName string
	EnvironmentSlug string
}

func (l *Logger) LogEnvironmentCreate(ctx context.Context, dbtx repo.DBTX, event LogEnvironmentCreateEvent) error {
	action := ActionEnvironmentCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.EnvironmentURN.ID.String(),
		SubjectType:        string(subjectTypeEnvironment),
		SubjectDisplayName: conv.ToPGTextEmpty(event.EnvironmentName),
		SubjectSlug:        conv.ToPGTextEmpty(event.EnvironmentSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogEnvironmentUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	EnvironmentURN            urn.Environment
	EnvironmentName           string
	EnvironmentSlug           string
	EnvironmentSnapshotBefore *types.Environment
	EnvironmentSnapshotAfter  *types.Environment
}

func (l *Logger) LogEnvironmentUpdate(ctx context.Context, dbtx repo.DBTX, event LogEnvironmentUpdateEvent) error {
	action := ActionEnvironmentUpdate

	beforeSnapshot, err := marshalAuditPayload(event.EnvironmentSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.EnvironmentSnapshotAfter)
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

		SubjectID:          event.EnvironmentURN.ID.String(),
		SubjectType:        string(subjectTypeEnvironment),
		SubjectDisplayName: conv.ToPGTextEmpty(event.EnvironmentName),
		SubjectSlug:        conv.ToPGTextEmpty(event.EnvironmentSlug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogEnvironmentDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	EnvironmentURN  urn.Environment
	EnvironmentName string
	EnvironmentSlug string
}

func (l *Logger) LogEnvironmentDelete(ctx context.Context, dbtx repo.DBTX, event LogEnvironmentDeleteEvent) error {
	action := ActionEnvironmentDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.EnvironmentURN.ID.String(),
		SubjectType:        "environment",
		SubjectDisplayName: conv.ToPGTextEmpty(event.EnvironmentName),
		SubjectSlug:        conv.ToPGTextEmpty(event.EnvironmentSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
