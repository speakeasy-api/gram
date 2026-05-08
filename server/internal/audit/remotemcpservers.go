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
	ActionRemoteMcpServerCreate Action = "remote-mcp:create"
	ActionRemoteMcpServerUpdate Action = "remote-mcp:update"
	ActionRemoteMcpServerDelete Action = "remote-mcp:delete"
)

type LogRemoteMcpServerCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RemoteMcpServerID  uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.RemoteMcpServer and migrate to RemoteMcpServerURN; pending team discussion
	RemoteMcpServerURL string
}

func (l *Logger) LogRemoteMcpServerCreate(ctx context.Context, dbtx repo.DBTX, event LogRemoteMcpServerCreateEvent) error {
	action := ActionRemoteMcpServerCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RemoteMcpServerID.String(),
		SubjectType:        string(subjectTypeRemoteMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.RemoteMcpServerURL),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogRemoteMcpServerUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RemoteMcpServerID  uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.RemoteMcpServer and migrate to RemoteMcpServerURN; pending team discussion
	RemoteMcpServerURL string
	SnapshotBefore     *types.RemoteMcpServer
	SnapshotAfter      *types.RemoteMcpServer
}

func (l *Logger) LogRemoteMcpServerUpdate(ctx context.Context, dbtx repo.DBTX, event LogRemoteMcpServerUpdateEvent) error {
	action := ActionRemoteMcpServerUpdate

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

		SubjectID:          event.RemoteMcpServerID.String(),
		SubjectType:        string(subjectTypeRemoteMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.RemoteMcpServerURL),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogRemoteMcpServerDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	RemoteMcpServerID  uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.RemoteMcpServer and migrate to RemoteMcpServerURN; pending team discussion
	RemoteMcpServerURL string
}

func (l *Logger) LogRemoteMcpServerDelete(ctx context.Context, dbtx repo.DBTX, event LogRemoteMcpServerDeleteEvent) error {
	action := ActionRemoteMcpServerDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.RemoteMcpServerID.String(),
		SubjectType:        string(subjectTypeRemoteMcpServer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.RemoteMcpServerURL),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
