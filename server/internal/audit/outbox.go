package audit

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

// PublishWebhook publishes the webhook event for an inserted entry of either
// log, in the caller's transaction. Staff actions are masked as the Speakeasy
// team, and category tells subscribers which log the entry is in.
func PublishWebhook(ctx context.Context, dbtx repo.DBTX, input repo.InsertAuditLogParams, result repo.InsertAuditLogRow, category Category, event *outbox.EventDef[events.AuditLogCreatedPayloadV1]) error {
	actorDisplayName := conv.FromPGTextOrEmpty[string](input.ActorDisplayName)
	actorSlug := conv.FromPGTextOrEmpty[string](input.ActorSlug)
	actingSurface := Surface(conv.FromPGTextOrEmpty[string](input.ActingSurface))
	actorID := input.ActorID
	actingClientID := conv.FromPGTextOrEmpty[string](input.ActingClientID)
	if IsStaffAdminSurface(actingSurface) {
		actorID = ""
		actorDisplayName = SpeakeasyTeamActorLabel
		actorSlug = ""
		actingClientID = ""
	}

	if _, err := outbox.PublishWebhookEvent(ctx, dbtx, result.OrganizationID, event, events.AuditLogCreatedPayloadV1{
		ID:                 result.ID,
		OrganizationID:     result.OrganizationID,
		ProjectID:          input.ProjectID,
		ActorID:            actorID,
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
		Category:           string(category),
		ActingClientID:     actingClientID,
	}); err != nil {
		return fmt.Errorf("log %s: append to outbox: %w", input.Action, err)
	}

	return nil
}
