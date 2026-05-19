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
	ActionAIIntegrationUpsert Action = "ai_integration:upsert"
	ActionAIIntegrationDelete Action = "ai_integration:delete"
)

// AIIntegrationSnapshot intentionally omits the API key. It only records
// whether a key was configured so audit consumers can see secret lifecycle
// changes without exposing the secret itself.
type AIIntegrationSnapshot struct {
	Provider  string    `json:"provider"`
	ProjectID uuid.UUID `json:"project_id"`
	Enabled   bool      `json:"enabled"`
	HasAPIKey bool      `json:"has_api_key"`
}

type LogAIIntegrationUpsertEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ConfigURN urn.AIIntegrationConfig

	SnapshotBefore *AIIntegrationSnapshot
	SnapshotAfter  *AIIntegrationSnapshot
}

func (l *Logger) LogAIIntegrationUpsert(ctx context.Context, dbtx repo.DBTX, event LogAIIntegrationUpsertEvent) error {
	action := ActionAIIntegrationUpsert

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
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: true},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ConfigURN.ID.String(),
		SubjectType:        string(subjectTypeAIIntegration),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, entry)
}

type LogAIIntegrationDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ConfigURN urn.AIIntegrationConfig
}

func (l *Logger) LogAIIntegrationDelete(ctx context.Context, dbtx repo.DBTX, event LogAIIntegrationDeleteEvent) error {
	action := ActionAIIntegrationDelete

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: true},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ConfigURN.ID.String(),
		SubjectType:        string(subjectTypeAIIntegration),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}

	return l.log(ctx, dbtx, entry)
}
