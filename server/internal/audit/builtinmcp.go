package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const ActionBuiltinMCPUpdate Action = "builtin_mcp:update"

// LogBuiltinMCPUpdate records only non-secret project configuration.
func (l *Logger) LogBuiltinMCPUpdate(ctx context.Context, db repo.DBTX, organizationID string, projectID, serverID uuid.UUID, key string, actor urn.Principal, before, after any) error {
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return fmt.Errorf("marshal built-in settings before update: %w", err)
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return fmt.Errorf("marshal built-in settings after update: %w", err)
	}
	return l.log(ctx, db, auditEntry{Params: repo.InsertAuditLogParams{
		OrganizationID:     organizationID,
		ProjectID:          uuid.NullUUID{UUID: projectID, Valid: true},
		ActorID:            actor.ID,
		ActorType:          string(actor.Type),
		ActorDisplayName:   conv.ToPGTextEmpty(""),
		ActorSlug:          conv.ToPGTextEmpty(""),
		Metadata:           nil,
		Action:             string(ActionBuiltinMCPUpdate),
		SubjectID:          serverID.String(),
		SubjectType:        string(subjectTypeBuiltinMcpServer),
		SubjectDisplayName: conv.ToPGText(key),
		SubjectSlug:        conv.ToPGText(key),
		BeforeSnapshot:     beforeJSON,
		AfterSnapshot:      afterJSON,
	}, OutboxEvent: events.McpServerV1})
}
