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
	ActionVariationUpdateGlobal Action = "variation:update_global"
	ActionVariationDeleteGlobal Action = "variation:delete_global"
)

type LogVariationUpdateGlobalEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	VariationURN  urn.Variation
	SourceToolURN urn.Tool

	VariationSnapshotBefore *types.ToolVariation
	VariationSnapshotAfter  *types.ToolVariation
}

func (l *Logger) LogVariationUpdateGlobal(ctx context.Context, dbtx repo.DBTX, event LogVariationUpdateGlobalEvent) error {
	action := ActionVariationUpdateGlobal

	metadata, err := marshalAuditPayload(map[string]any{
		"src_tool_urn": event.SourceToolURN.String(),
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	beforeSnapshot, err := marshalAuditPayload(event.VariationSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.VariationSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.VariationURN.ID.String(),
		SubjectType:        string(subjectTypeVariation),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SourceToolURN.Name),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       metadata,
		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
	}
	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogVariationDeleteGlobalEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	VariationURN  urn.Variation
	SourceToolURN urn.Tool
}

func (l *Logger) LogVariationDeleteGlobal(ctx context.Context, dbtx repo.DBTX, event LogVariationDeleteGlobalEvent) error {
	action := ActionVariationDeleteGlobal

	metadata, err := marshalAuditPayload(map[string]any{
		"src_tool_urn": event.SourceToolURN.String(),
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.VariationURN.ID.String(),
		SubjectType:        string(subjectTypeVariation),
		SubjectDisplayName: conv.ToPGTextEmpty(event.SourceToolURN.Name),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}
	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
