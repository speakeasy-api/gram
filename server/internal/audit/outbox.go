package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox"
)

type outboxEntry struct {
	ID                 uuid.UUID       `json:"id,omitzero"`
	OrganizationID     string          `json:"organization_id,omitzero"`
	ProjectID          uuid.NullUUID   `json:"project_id,omitzero"`
	ActorID            string          `json:"actor_id,omitzero"`
	ActorType          string          `json:"actor_type,omitzero"`
	ActorDisplayName   string          `json:"actor_display_name,omitzero"`
	ActorSlug          string          `json:"actor_slug,omitzero"`
	Action             string          `json:"action,omitzero"`
	SubjectID          string          `json:"subject_id,omitzero"`
	SubjectType        string          `json:"subject_type,omitzero"`
	SubjectDisplayName string          `json:"subject_display_name,omitzero"`
	SubjectSlug        string          `json:"subject_slug,omitzero"`
	BeforeSnapshot     json.RawMessage `json:"before_snapshot,omitempty"`
	AfterSnapshot      json.RawMessage `json:"after_snapshot,omitempty"`
	Metadata           json.RawMessage `json:"metadata,omitempty"`
}

func appendToOutbox(ctx context.Context, dbtx repo.DBTX, input repo.InsertAuditLogParams, result repo.InsertAuditLogRow) error {
	if _, err := outbox.Append(ctx, dbtx, outbox.AppendParams{
		OrganizationID: result.OrganizationID,
		EventType:      outbox.EventTypeAuditLogCreated,
		Payload: outboxEntry{
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
		},
	}); err != nil {
		return fmt.Errorf("append to outbox: %w", err)
	}

	return nil
}
