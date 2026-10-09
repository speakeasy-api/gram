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
	ActionEnvironmentCreate Action = "environment:create"
	ActionEnvironmentUpdate Action = "environment:update"
	ActionEnvironmentDelete Action = "environment:delete"

	// Binding an environment to a source or toolset hands it that
	// environment's values. The subject is the environment; a replacement is
	// an unlink of the old environment followed by a link of the new one.
	ActionEnvironmentSourceLink    Action = "environment:link-source"
	ActionEnvironmentSourceUnlink  Action = "environment:unlink-source"
	ActionEnvironmentToolsetLink   Action = "environment:link-toolset"
	ActionEnvironmentToolsetUnlink Action = "environment:unlink-toolset"
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

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.EnvironmentV1})
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

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.EnvironmentV1})
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

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.EnvironmentV1})
}

type LogEnvironmentSourceLinkEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	EnvironmentURN  urn.Environment
	EnvironmentName string
	EnvironmentSlug string

	SourceKind string
	SourceSlug string
}

// LogEnvironmentSourceLink records an environment being bound to a source.
func (l *Logger) LogEnvironmentSourceLink(ctx context.Context, dbtx repo.DBTX, event LogEnvironmentSourceLinkEvent) error {
	return l.logEnvironmentBinding(ctx, dbtx, ActionEnvironmentSourceLink, event.OrganizationID, event.ProjectID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.EnvironmentURN, event.EnvironmentName, event.EnvironmentSlug,
		map[string]any{"source_kind": event.SourceKind, "source_slug": event.SourceSlug})
}

type LogEnvironmentSourceUnlinkEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	EnvironmentURN  urn.Environment
	EnvironmentName string
	EnvironmentSlug string

	SourceKind string
	SourceSlug string
}

// LogEnvironmentSourceUnlink records a source's binding to an environment
// being removed or replaced.
func (l *Logger) LogEnvironmentSourceUnlink(ctx context.Context, dbtx repo.DBTX, event LogEnvironmentSourceUnlinkEvent) error {
	return l.logEnvironmentBinding(ctx, dbtx, ActionEnvironmentSourceUnlink, event.OrganizationID, event.ProjectID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.EnvironmentURN, event.EnvironmentName, event.EnvironmentSlug,
		map[string]any{"source_kind": event.SourceKind, "source_slug": event.SourceSlug})
}

type LogEnvironmentToolsetLinkEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	EnvironmentURN  urn.Environment
	EnvironmentName string
	EnvironmentSlug string

	ToolsetURN urn.Toolset
}

// LogEnvironmentToolsetLink records an environment being bound to a toolset.
func (l *Logger) LogEnvironmentToolsetLink(ctx context.Context, dbtx repo.DBTX, event LogEnvironmentToolsetLinkEvent) error {
	return l.logEnvironmentBinding(ctx, dbtx, ActionEnvironmentToolsetLink, event.OrganizationID, event.ProjectID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.EnvironmentURN, event.EnvironmentName, event.EnvironmentSlug,
		map[string]any{"toolset_id": event.ToolsetURN.ID.String()})
}

type LogEnvironmentToolsetUnlinkEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	EnvironmentURN  urn.Environment
	EnvironmentName string
	EnvironmentSlug string

	ToolsetURN urn.Toolset
}

// LogEnvironmentToolsetUnlink records a toolset's binding to an environment
// being removed or replaced.
func (l *Logger) LogEnvironmentToolsetUnlink(ctx context.Context, dbtx repo.DBTX, event LogEnvironmentToolsetUnlinkEvent) error {
	return l.logEnvironmentBinding(ctx, dbtx, ActionEnvironmentToolsetUnlink, event.OrganizationID, event.ProjectID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.EnvironmentURN, event.EnvironmentName, event.EnvironmentSlug,
		map[string]any{"toolset_id": event.ToolsetURN.ID.String()})
}

func (l *Logger) logEnvironmentBinding(ctx context.Context, dbtx repo.DBTX, action Action, organizationID string, projectID uuid.UUID, actor urn.Principal, actorDisplayName, actorSlug *string, environment urn.Environment, name, slug string, target map[string]any) error {
	metadata, err := marshalAuditPayload(target)
	if err != nil {
		return fmt.Errorf("build %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: projectID, Valid: projectID != uuid.Nil},

		ActorID:          actor.ID,
		ActorType:        string(actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(actorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(actorSlug),

		Action: string(action),

		SubjectID:          environment.ID.String(),
		SubjectType:        string(subjectTypeEnvironment),
		SubjectDisplayName: conv.ToPGTextEmpty(name),
		SubjectSlug:        conv.ToPGTextEmpty(slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.EnvironmentV1})
}
