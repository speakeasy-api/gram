package audit

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

func appendToOutbox(ctx context.Context, dbtx repo.DBTX, input repo.InsertAuditLogParams, result repo.InsertAuditLogRow) error {
	if _, err := outbox.Append(ctx, dbtx, result.OrganizationID, events.AuditLogCreated, events.AuditLogCreatedPayload{
		ID:                 result.ID,
		OrganizationID:     result.OrganizationID,
		ProjectID:          input.ProjectID,
		ActorID:            input.ActorID,
		ActorType:          input.ActorType,
		ActorDisplayName:   conv.FromPGTextOrEmpty[string](input.ActorDisplayName),
		ActorSlug:          conv.FromPGTextOrEmpty[string](input.ActorSlug),
		Action:             input.Action,
		SubjectID:          input.SubjectID,
		SubjectType:        input.SubjectType,
		SubjectDisplayName: conv.FromPGTextOrEmpty[string](input.SubjectDisplayName),
		SubjectSlug:        conv.FromPGTextOrEmpty[string](input.SubjectSlug),
		BeforeSnapshot:     input.BeforeSnapshot,
		AfterSnapshot:      input.AfterSnapshot,
		Metadata:           input.Metadata,
	}); err != nil {
		return fmt.Errorf("append to outbox: %w", err)
	}

	return nil
}
