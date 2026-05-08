package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionPluginCreate         Action = "plugin:create"
	ActionPluginUpdate         Action = "plugin:update"
	ActionPluginDelete         Action = "plugin:delete"
	ActionPluginServerAdd      Action = "plugin:server_add"
	ActionPluginServerUpdate   Action = "plugin:server_update"
	ActionPluginServerRemove   Action = "plugin:server_remove"
	ActionPluginAssignmentsSet Action = "plugin:assignments_set"
	ActionPluginPublish        Action = "plugin:publish"
)

// PluginSnapshot captures the user-meaningful state of a plugin row for
// before/after comparisons in audit log entries.
type PluginSnapshot struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description,omitempty"`
}

type LogPluginCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	PluginID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Plugin and migrate to PluginURN; pending team discussion
	PluginName string
	PluginSlug string
}

func (l *Logger) LogPluginCreate(ctx context.Context, dbtx repo.DBTX, event LogPluginCreateEvent) error {
	action := ActionPluginCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.PluginID.String(),
		SubjectType:        string(subjectTypePlugin),
		SubjectDisplayName: conv.ToPGTextEmpty(event.PluginName),
		SubjectSlug:        conv.ToPGTextEmpty(event.PluginSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogPluginUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	PluginID       uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Plugin and migrate to PluginURN; pending team discussion
	PluginName     string
	PluginSlug     string
	SnapshotBefore *PluginSnapshot
	SnapshotAfter  *PluginSnapshot
}

func (l *Logger) LogPluginUpdate(ctx context.Context, dbtx repo.DBTX, event LogPluginUpdateEvent) error {
	action := ActionPluginUpdate

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

		SubjectID:          event.PluginID.String(),
		SubjectType:        string(subjectTypePlugin),
		SubjectDisplayName: conv.ToPGTextEmpty(event.PluginName),
		SubjectSlug:        conv.ToPGTextEmpty(event.PluginSlug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogPluginDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	PluginID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Plugin and migrate to PluginURN; pending team discussion
	PluginName string
	PluginSlug string
}

func (l *Logger) LogPluginDelete(ctx context.Context, dbtx repo.DBTX, event LogPluginDeleteEvent) error {
	action := ActionPluginDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.PluginID.String(),
		SubjectType:        string(subjectTypePlugin),
		SubjectDisplayName: conv.ToPGTextEmpty(event.PluginName),
		SubjectSlug:        conv.ToPGTextEmpty(event.PluginSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogPluginServerAddEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	PluginID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Plugin and migrate to PluginURN; pending team discussion
	PluginName string
	PluginSlug string

	ServerID          uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.PluginServer and migrate to ServerURN; pending team discussion
	ServerDisplayName string
	ServerPolicy      string
	ServerSortOrder   int32
	ToolsetURN        urn.Toolset
}

func (l *Logger) LogPluginServerAdd(ctx context.Context, dbtx repo.DBTX, event LogPluginServerAddEvent) error {
	action := ActionPluginServerAdd

	metadata, err := marshalAuditPayload(map[string]any{
		"server_id":           event.ServerID.String(),
		"server_display_name": event.ServerDisplayName,
		"server_policy":       event.ServerPolicy,
		"server_sort_order":   event.ServerSortOrder,
		"toolset_urn":         event.ToolsetURN.String(),
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.PluginID.String(),
		SubjectType:        string(subjectTypePlugin),
		SubjectDisplayName: conv.ToPGTextEmpty(event.PluginName),
		SubjectSlug:        conv.ToPGTextEmpty(event.PluginSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogPluginServerUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	PluginID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Plugin and migrate to PluginURN; pending team discussion
	PluginName string
	PluginSlug string

	ServerID          uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.PluginServer and migrate to ServerURN; pending team discussion
	ServerDisplayName string
	ServerPolicy      string
	ServerSortOrder   int32
}

func (l *Logger) LogPluginServerUpdate(ctx context.Context, dbtx repo.DBTX, event LogPluginServerUpdateEvent) error {
	action := ActionPluginServerUpdate

	metadata, err := marshalAuditPayload(map[string]any{
		"server_id":           event.ServerID.String(),
		"server_display_name": event.ServerDisplayName,
		"server_policy":       event.ServerPolicy,
		"server_sort_order":   event.ServerSortOrder,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.PluginID.String(),
		SubjectType:        string(subjectTypePlugin),
		SubjectDisplayName: conv.ToPGTextEmpty(event.PluginName),
		SubjectSlug:        conv.ToPGTextEmpty(event.PluginSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogPluginServerRemoveEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	PluginID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Plugin and migrate to PluginURN; pending team discussion
	PluginName string
	PluginSlug string

	ServerID uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.PluginServer and migrate to ServerURN; pending team discussion
}

func (l *Logger) LogPluginServerRemove(ctx context.Context, dbtx repo.DBTX, event LogPluginServerRemoveEvent) error {
	action := ActionPluginServerRemove

	metadata, err := marshalAuditPayload(map[string]any{
		"server_id": event.ServerID.String(),
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.PluginID.String(),
		SubjectType:        string(subjectTypePlugin),
		SubjectDisplayName: conv.ToPGTextEmpty(event.PluginName),
		SubjectSlug:        conv.ToPGTextEmpty(event.PluginSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogPluginAssignmentsSetEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	PluginID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Plugin and migrate to PluginURN; pending team discussion
	PluginName string
	PluginSlug string

	PrincipalURNs []string
}

func (l *Logger) LogPluginAssignmentsSet(ctx context.Context, dbtx repo.DBTX, event LogPluginAssignmentsSetEvent) error {
	action := ActionPluginAssignmentsSet

	metadata, err := marshalAuditPayload(map[string]any{
		"principal_urns": event.PrincipalURNs,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.PluginID.String(),
		SubjectType:        string(subjectTypePlugin),
		SubjectDisplayName: conv.ToPGTextEmpty(event.PluginName),
		SubjectSlug:        conv.ToPGTextEmpty(event.PluginSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

// LogPluginPublishEvent records a single user-initiated publish of all
// plugins in a project to GitHub. The event is project-scoped because the
// publish creates one repo for all plugins together; per-plugin slugs are
// captured in metadata.
type LogPluginPublishEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID
	ProjectName    string
	ProjectSlug    string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	PluginSlugs []string
	RepoOwner   string
	RepoName    string
}

func (l *Logger) LogPluginPublish(ctx context.Context, dbtx repo.DBTX, event LogPluginPublishEvent) error {
	action := ActionPluginPublish

	metadata, err := marshalAuditPayload(map[string]any{
		"plugin_slugs": event.PluginSlugs,
		"repo_owner":   event.RepoOwner,
		"repo_name":    event.RepoName,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
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

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
