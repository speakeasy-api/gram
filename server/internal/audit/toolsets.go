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
	ActionToolsetCreate              Action = "toolset:create"
	ActionToolsetUpdate              Action = "toolset:update"
	ActionToolsetDelete              Action = "toolset:delete"
	ActionToolsetAttachExternalOAuth Action = "toolset:attach_external_oauth"
	ActionToolsetDetachExternalOAuth Action = "toolset:detach_external_oauth"
	ActionToolsetAttachOAuthProxy    Action = "toolset:attach_oauth_proxy"
	ActionToolsetUpdateOAuthProxy    Action = "toolset:update_oauth_proxy"
	ActionToolsetDetachOAuthProxy    Action = "toolset:detach_oauth_proxy"
)

type LogToolsetCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ToolsetURN  urn.Toolset
	ToolsetName string
	ToolsetSlug string
}

func (l *Logger) LogToolsetCreate(ctx context.Context, dbtx repo.DBTX, event LogToolsetCreateEvent) error {
	action := ActionToolsetCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ToolsetURN.ID.String(),
		SubjectType:        string(subjectTypeToolset),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ToolsetName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ToolsetSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogToolsetUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ToolsetURN            urn.Toolset
	ToolsetName           string
	ToolsetSlug           string
	ToolsetVersionAfter   int64
	ToolsetSnapshotBefore *types.Toolset
	ToolsetSnapshotAfter  *types.Toolset
}

func (l *Logger) LogToolsetUpdate(ctx context.Context, dbtx repo.DBTX, event LogToolsetUpdateEvent) error {
	action := ActionToolsetUpdate

	// Clone snapshots and strip the Tools field to avoid serializing a potentially massive list of tools into the audit log.
	var snapshotBefore, snapshotAfter *types.Toolset
	if event.ToolsetSnapshotBefore != nil {
		clone := *event.ToolsetSnapshotBefore
		clone.Tools = nil
		snapshotBefore = &clone
	}
	if event.ToolsetSnapshotAfter != nil {
		clone := *event.ToolsetSnapshotAfter
		clone.Tools = nil
		snapshotAfter = &clone
	}

	beforeSnapshot, err := marshalAuditPayload(snapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(snapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	metadata, err := marshalAuditPayload(map[string]any{
		"toolset_version_after": event.ToolsetVersionAfter,
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

		SubjectID:          event.ToolsetURN.ID.String(),
		SubjectType:        string(subjectTypeToolset),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ToolsetName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ToolsetSlug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogToolsetDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ToolsetURN  urn.Toolset
	ToolsetName string
	ToolsetSlug string
}

func (l *Logger) LogToolsetDelete(ctx context.Context, dbtx repo.DBTX, event LogToolsetDeleteEvent) error {
	action := ActionToolsetDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ToolsetURN.ID.String(),
		SubjectType:        string(subjectTypeToolset),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ToolsetName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ToolsetSlug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogToolsetAttachExternalOAuthEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ToolsetURN          urn.Toolset
	ToolsetName         string
	ToolsetSlug         string
	ToolsetVersionAfter int64

	ExternalOAuthServerID   string //nolint:glint // TODO(AGE-1954): discuss URN treatment for external OAuth server identifiers; pending team discussion
	ExternalOAuthServerSlug string
}

func (l *Logger) LogToolsetAttachExternalOAuth(ctx context.Context, dbtx repo.DBTX, event LogToolsetAttachExternalOAuthEvent) error {
	action := ActionToolsetAttachExternalOAuth

	metadata, err := marshalAuditPayload(map[string]any{
		"toolset_version_after":      event.ToolsetVersionAfter,
		"external_oauth_server_id":   event.ExternalOAuthServerID,
		"external_oauth_server_slug": event.ExternalOAuthServerSlug,
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

		SubjectID:          event.ToolsetURN.ID.String(),
		SubjectType:        string(subjectTypeToolset),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ToolsetName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ToolsetSlug),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogToolsetDetachExternalOAuthEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ToolsetURN          urn.Toolset
	ToolsetName         string
	ToolsetSlug         string
	ToolsetVersionAfter int64

	ExternalOAuthServerID   *string //nolint:glint // TODO(AGE-1954): discuss URN treatment for external OAuth server identifiers; pending team discussion
	ExternalOAuthServerSlug *string
}

func (l *Logger) LogToolsetDetachExternalOAuth(ctx context.Context, dbtx repo.DBTX, event LogToolsetDetachExternalOAuthEvent) error {
	action := ActionToolsetDetachExternalOAuth

	metadata, err := marshalAuditPayload(map[string]any{
		"external_oauth_server_id":   event.ExternalOAuthServerID,
		"external_oauth_server_slug": event.ExternalOAuthServerSlug,
		"toolset_version_after":      event.ToolsetVersionAfter,
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

		SubjectID:          event.ToolsetURN.ID.String(),
		SubjectType:        string(subjectTypeToolset),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ToolsetName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ToolsetSlug),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogToolsetAttachOAuthProxyEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ToolsetURN          urn.Toolset
	ToolsetName         string
	ToolsetSlug         string
	ToolsetVersionAfter int64

	OAuthProxyServerID   string //nolint:glint // TODO(AGE-1954): discuss URN treatment for OAuth proxy server identifiers; pending team discussion
	OAuthProxyServerSlug string
}

func (l *Logger) LogToolsetAttachOAuthProxy(ctx context.Context, dbtx repo.DBTX, event LogToolsetAttachOAuthProxyEvent) error {
	action := ActionToolsetAttachOAuthProxy

	metadata, err := marshalAuditPayload(map[string]any{
		"oauth_proxy_server_id":   event.OAuthProxyServerID,
		"oauth_proxy_server_slug": event.OAuthProxyServerSlug,
		"toolset_version_after":   event.ToolsetVersionAfter,
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

		SubjectID:          event.ToolsetURN.ID.String(),
		SubjectType:        string(subjectTypeToolset),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ToolsetName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ToolsetSlug),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogToolsetDetachOAuthProxyEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ToolsetURN          urn.Toolset
	ToolsetName         string
	ToolsetSlug         string
	ToolsetVersionAfter int64

	OAuthProxyServerID   *string //nolint:glint // TODO(AGE-1954): discuss URN treatment for OAuth proxy server identifiers; pending team discussion
	OAuthProxyServerSlug *string
}

func (l *Logger) LogToolsetDetachOAuthProxy(ctx context.Context, dbtx repo.DBTX, event LogToolsetDetachOAuthProxyEvent) error {
	action := ActionToolsetDetachOAuthProxy

	metadata, err := marshalAuditPayload(map[string]any{
		"oauth_proxy_server_id":   event.OAuthProxyServerID,
		"oauth_proxy_server_slug": event.OAuthProxyServerSlug,
		"toolset_version_after":   event.ToolsetVersionAfter,
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

		SubjectID:          event.ToolsetURN.ID.String(),
		SubjectType:        string(subjectTypeToolset),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ToolsetName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ToolsetSlug),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogToolsetUpdateOAuthProxyEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ToolsetURN          urn.Toolset
	ToolsetName         string
	ToolsetSlug         string
	ToolsetVersionAfter int64

	OAuthProxyServerID   string //nolint:glint // TODO(AGE-1954): discuss URN treatment for OAuth proxy server identifiers; pending team discussion
	OAuthProxyServerSlug string

	ToolsetSnapshotBefore *types.Toolset
	ToolsetSnapshotAfter  *types.Toolset
}

func (l *Logger) LogToolsetUpdateOAuthProxy(ctx context.Context, dbtx repo.DBTX, event LogToolsetUpdateOAuthProxyEvent) error {
	action := ActionToolsetUpdateOAuthProxy

	// Clone snapshots and strip the Tools field to avoid serializing a potentially massive list of tools into the audit log.
	var snapshotBefore, snapshotAfter *types.Toolset
	if event.ToolsetSnapshotBefore != nil {
		clone := *event.ToolsetSnapshotBefore
		clone.Tools = nil
		snapshotBefore = &clone
	}
	if event.ToolsetSnapshotAfter != nil {
		clone := *event.ToolsetSnapshotAfter
		clone.Tools = nil
		snapshotAfter = &clone
	}

	beforeSnapshot, err := marshalAuditPayload(snapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(snapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	metadata, err := marshalAuditPayload(map[string]any{
		"oauth_proxy_server_id":   event.OAuthProxyServerID,
		"oauth_proxy_server_slug": event.OAuthProxyServerSlug,
		"toolset_version_after":   event.ToolsetVersionAfter,
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

		SubjectID:          event.ToolsetURN.ID.String(),
		SubjectType:        string(subjectTypeToolset),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ToolsetName),
		SubjectSlug:        conv.ToPGTextEmpty(event.ToolsetSlug),

		Metadata:       metadata,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
