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
	ActionMcpSlugCreate Action = "mcp-slug:create"
	ActionMcpSlugUpdate Action = "mcp-slug:update"
	ActionMcpSlugDelete Action = "mcp-slug:delete"
)

type LogMcpSlugCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpSlugURN urn.McpSlug
	Slug       string
}

func LogMcpSlugCreate(ctx context.Context, dbtx repo.DBTX, event LogMcpSlugCreateEvent) error {
	action := ActionMcpSlugCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.McpSlugURN.ID.String(),
		SubjectType:        string(subjectTypeMcpSlug),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Slug),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogMcpSlugUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpSlugURN            urn.McpSlug
	Slug                  string
	McpSlugSnapshotBefore *types.McpSlug
	McpSlugSnapshotAfter  *types.McpSlug
}

func LogMcpSlugUpdate(ctx context.Context, dbtx repo.DBTX, event LogMcpSlugUpdateEvent) error {
	action := ActionMcpSlugUpdate

	beforeSnapshot, err := marshalAuditPayload(event.McpSlugSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.McpSlugSnapshotAfter)
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

		SubjectID:          event.McpSlugURN.ID.String(),
		SubjectType:        string(subjectTypeMcpSlug),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Slug),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogMcpSlugDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpSlugURN urn.McpSlug
	Slug       string
}

func LogMcpSlugDelete(ctx context.Context, dbtx repo.DBTX, event LogMcpSlugDeleteEvent) error {
	action := ActionMcpSlugDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.McpSlugURN.ID.String(),
		SubjectType:        string(subjectTypeMcpSlug),
		SubjectDisplayName: conv.ToPGTextEmpty(event.Slug),
		SubjectSlug:        conv.ToPGTextEmpty(event.Slug),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
