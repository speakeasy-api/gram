package audit

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

func appendToOutbox(ctx context.Context, dbtx repo.DBTX, entry auditEntry, result repo.InsertAuditLogRow) error {
	input := entry.Params
	actorDisplayName := conv.FromPGTextOrEmpty[string](input.ActorDisplayName)
	actorSlug := conv.FromPGTextOrEmpty[string](input.ActorSlug)
	actingSurface := Surface(conv.FromPGTextOrEmpty[string](input.ActingSurface))
	if IsStaffAdminSurface(actingSurface) {
		actorDisplayName = SpeakeasyTeamActorLabel
		actorSlug = ""
	}
	actingClientID := conv.FromPGTextOrEmpty[string](input.ActingClientID)
	if IsStaffAdminSurface(actingSurface) {
		actingClientID = ""
	}

	if _, err := outbox.PublishWebhookEvent(ctx, dbtx, result.OrganizationID, entry.OutboxEvent, events.AuditLogCreatedPayloadV1{
		ID:                 result.ID,
		OrganizationID:     result.OrganizationID,
		ProjectID:          input.ProjectID,
		ActorID:            input.ActorID,
		ActorType:          input.ActorType,
		ActorDisplayName:   actorDisplayName,
		ActorSlug:          actorSlug,
		Action:             input.Action,
		SubjectID:          input.SubjectID,
		SubjectType:        input.SubjectType,
		SubjectDisplayName: conv.FromPGTextOrEmpty[string](input.SubjectDisplayName),
		SubjectSlug:        conv.FromPGTextOrEmpty[string](input.SubjectSlug),
		BeforeSnapshot:     input.BeforeSnapshot,
		AfterSnapshot:      input.AfterSnapshot,
		Metadata:           input.Metadata,
		ActingSurface:      conv.FromPGTextOrEmpty[string](input.ActingSurface),
		ActingClientID:     actingClientID,
	}); err != nil {
		return fmt.Errorf("append to outbox: %w", err)
	}

	return nil
}
