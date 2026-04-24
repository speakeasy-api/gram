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
	ActionMcpFrontendCreate Action = "mcp-frontend:create"
	ActionMcpFrontendUpdate Action = "mcp-frontend:update"
	ActionMcpFrontendDelete Action = "mcp-frontend:delete"
)

type LogMcpFrontendCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpFrontendID uuid.UUID
}

func LogMcpFrontendCreate(ctx context.Context, dbtx repo.DBTX, event LogMcpFrontendCreateEvent) error {
	action := ActionMcpFrontendCreate
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.McpFrontendID.String(),
		SubjectType:        string(subjectTypeMcpFrontend),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
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

type LogMcpFrontendUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpFrontendID  uuid.UUID
	SnapshotBefore any
	SnapshotAfter  any
}

func LogMcpFrontendUpdate(ctx context.Context, dbtx repo.DBTX, event LogMcpFrontendUpdateEvent) error {
	action := ActionMcpFrontendUpdate

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

		SubjectID:          event.McpFrontendID.String(),
		SubjectType:        string(subjectTypeMcpFrontend),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
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

type LogMcpFrontendDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	McpFrontendID uuid.UUID
}

func LogMcpFrontendDelete(ctx context.Context, dbtx repo.DBTX, event LogMcpFrontendDeleteEvent) error {
	action := ActionMcpFrontendDelete
	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.McpFrontendID.String(),
		SubjectType:        string(subjectTypeMcpFrontend),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
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
