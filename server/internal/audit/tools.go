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
	ActionToolExecuteDestructiveDeny Action = "tool:execute_destructive_denied"
)

type LogToolExecuteDestructiveDenyEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	// ToolName is the raw tool identifier from the hook payload. For MCP-routed
	// calls this is "mcp__<server>__<tool>"; for native Claude Code tools it is
	// "Bash", "Edit", etc.
	ToolName string
	// Reason is a human-readable summary of the matched destructive pattern.
	Reason string
	// MatchedPattern, if non-empty, is the curated pattern slug ("category/name")
	// that fired the content trigger.
	MatchedPattern string
}

func LogToolExecuteDestructiveDeny(ctx context.Context, dbtx repo.DBTX, event LogToolExecuteDestructiveDenyEvent) error {
	action := ActionToolExecuteDestructiveDeny

	metadata, err := marshalAuditPayload(map[string]string{
		"reason":          event.Reason,
		"matched_pattern": event.MatchedPattern,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ToolName,
		SubjectType:        string(subjectTypeToolCall),
		SubjectDisplayName: conv.ToPGTextEmpty(event.ToolName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
