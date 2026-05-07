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
	ActionTemplateCreate Action = "template:create"
	ActionTemplateUpdate Action = "template:update"
	ActionTemplateDelete Action = "template:delete"
)

type LogTemplateCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TemplateID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Template and migrate to TemplateURN; pending team discussion
	TemplateURN  urn.Tool
	TemplateName string
}

func (l *Logger) LogTemplateCreate(ctx context.Context, dbtx repo.DBTX, event LogTemplateCreateEvent) error {
	action := ActionTemplateCreate

	metadata, err := marshalAuditPayload(map[string]any{
		"template_urn": event.TemplateURN.String(),
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

		SubjectID:          event.TemplateID.String(),
		SubjectType:        string(subjectTypeTemplate),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TemplateName),
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

type LogTemplateUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TemplateID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Template and migrate to TemplateURN; pending team discussion
	TemplateURN  urn.Tool
	TemplateName string
}

func (l *Logger) LogTemplateUpdate(ctx context.Context, dbtx repo.DBTX, event LogTemplateUpdateEvent) error {
	action := ActionTemplateUpdate

	metadata, err := marshalAuditPayload(map[string]any{
		"template_urn": event.TemplateURN.String(),
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

		SubjectID:          event.TemplateID.String(),
		SubjectType:        string(subjectTypeTemplate),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TemplateName),
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

type LogTemplateDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	TemplateID   uuid.UUID //nolint:glint // TODO(AGE-1954): introduce urn.Template and migrate to TemplateURN; pending team discussion
	TemplateURN  urn.Tool
	TemplateName string
}

func (l *Logger) LogTemplateDelete(ctx context.Context, dbtx repo.DBTX, event LogTemplateDeleteEvent) error {
	action := ActionTemplateDelete

	metadata, err := marshalAuditPayload(map[string]any{
		"template_urn": event.TemplateURN.String(),
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

		SubjectID:          event.TemplateID.String(),
		SubjectType:        string(subjectTypeTemplate),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TemplateName),
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
