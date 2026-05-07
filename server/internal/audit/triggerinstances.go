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
	ActionTriggerInstanceCreate Action = "trigger-instance:create"
	ActionTriggerInstanceUpdate Action = "trigger-instance:update"
	ActionTriggerInstanceDelete Action = "trigger-instance:delete"
	ActionTriggerInstancePause  Action = "trigger-instance:pause"
	ActionTriggerInstanceResume Action = "trigger-instance:resume"
)

type LogTriggerInstanceCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TriggerInstanceURN urn.TriggerInstance
	Name               string
	DefinitionSlug     string
}

func (l *Logger) LogTriggerInstanceCreate(ctx context.Context, dbtx repo.DBTX, event LogTriggerInstanceCreateEvent) error {
	action := ActionTriggerInstanceCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.TriggerInstanceURN.ID.String(),
		SubjectType:        string(subjectTypeTriggerInstance),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Name),
		SubjectSlug:        conv.ToPGTextEmpty(event.DefinitionSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogTriggerInstanceUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TriggerInstanceURN            urn.TriggerInstance
	Name                          string
	DefinitionSlug                string
	TriggerInstanceSnapshotBefore *types.TriggerInstance
	TriggerInstanceSnapshotAfter  *types.TriggerInstance
}

func (l *Logger) LogTriggerInstanceUpdate(ctx context.Context, dbtx repo.DBTX, event LogTriggerInstanceUpdateEvent) error {
	action := ActionTriggerInstanceUpdate

	beforeSnapshot, err := marshalAuditPayload(event.TriggerInstanceSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.TriggerInstanceSnapshotAfter)
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

		SubjectID:          event.TriggerInstanceURN.ID.String(),
		SubjectType:        string(subjectTypeTriggerInstance),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Name),
		SubjectSlug:        conv.ToPGTextEmpty(event.DefinitionSlug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogTriggerInstanceDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TriggerInstanceURN urn.TriggerInstance
	Name               string
	DefinitionSlug     string
}

func (l *Logger) LogTriggerInstanceDelete(ctx context.Context, dbtx repo.DBTX, event LogTriggerInstanceDeleteEvent) error {
	action := ActionTriggerInstanceDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.TriggerInstanceURN.ID.String(),
		SubjectType:        string(subjectTypeTriggerInstance),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Name),
		SubjectSlug:        conv.ToPGTextEmpty(event.DefinitionSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogTriggerInstancePauseEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TriggerInstanceURN urn.TriggerInstance
	Name               string
	DefinitionSlug     string
}

func (l *Logger) LogTriggerInstancePause(ctx context.Context, dbtx repo.DBTX, event LogTriggerInstancePauseEvent) error {
	action := ActionTriggerInstancePause
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.TriggerInstanceURN.ID.String(),
		SubjectType:        string(subjectTypeTriggerInstance),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Name),
		SubjectSlug:        conv.ToPGTextEmpty(event.DefinitionSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogTriggerInstanceResumeEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TriggerInstanceURN urn.TriggerInstance
	Name               string
	DefinitionSlug     string
}

func (l *Logger) LogTriggerInstanceResume(ctx context.Context, dbtx repo.DBTX, event LogTriggerInstanceResumeEvent) error {
	action := ActionTriggerInstanceResume
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.TriggerInstanceURN.ID.String(),
		SubjectType:        string(subjectTypeTriggerInstance),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Name),
		SubjectSlug:        conv.ToPGTextEmpty(event.DefinitionSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
