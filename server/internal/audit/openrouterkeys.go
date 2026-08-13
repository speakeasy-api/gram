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

// The platform-admin encrypt (scrub) action is deliberately not audited: it
// is internal storage hygiene with no customer-observable effect, and audit
// entries are exposed to the owning organization through the auditlogs API.
const (
	ActionOpenRouterAPIKeyDisable Action = "openrouter-key:disable"
	ActionOpenRouterAPIKeyEnable  Action = "openrouter-key:enable"
)

// openRouterAPIKeyMetadata carries the key type so consumers can tell the
// chat and internal key series apart without parsing the subject id. Key
// material never appears in audit entries.
type openRouterAPIKeyMetadata struct {
	KeyType string `json:"key_type"`
}

type LogOpenRouterAPIKeyDisableEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OpenRouterAPIKeyURN urn.OpenRouterAPIKey

	KeyType string
}

// LogOpenRouterAPIKeyDisable records the platform-admin lockdown of the key,
// upstream and locally.
func (l *Logger) LogOpenRouterAPIKeyDisable(ctx context.Context, dbtx repo.DBTX, event LogOpenRouterAPIKeyDisableEvent) error {
	return l.logOpenRouterAPIKeyEvent(ctx, dbtx, ActionOpenRouterAPIKeyDisable, openRouterAPIKeyEventFields(event))
}

type LogOpenRouterAPIKeyEnableEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OpenRouterAPIKeyURN urn.OpenRouterAPIKey

	KeyType string
}

// LogOpenRouterAPIKeyEnable records the platform-admin reinstatement of a
// disabled key.
func (l *Logger) LogOpenRouterAPIKeyEnable(ctx context.Context, dbtx repo.DBTX, event LogOpenRouterAPIKeyEnableEvent) error {
	return l.logOpenRouterAPIKeyEvent(ctx, dbtx, ActionOpenRouterAPIKeyEnable, openRouterAPIKeyEventFields(event))
}

type openRouterAPIKeyEventFields struct {
	OrganizationID      string
	Actor               urn.Principal
	ActorDisplayName    *string
	ActorSlug           *string
	OpenRouterAPIKeyURN urn.OpenRouterAPIKey
	KeyType             string
}

func (l *Logger) logOpenRouterAPIKeyEvent(ctx context.Context, dbtx repo.DBTX, action Action, fields openRouterAPIKeyEventFields) error {
	metadata, err := json.Marshal(openRouterAPIKeyMetadata{KeyType: fields.KeyType})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: fields.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          fields.Actor.ID,
		ActorType:        string(fields.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(fields.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(fields.ActorSlug),

		Action: string(action),

		SubjectID:          fields.OpenRouterAPIKeyURN.ID,
		SubjectType:        string(subjectTypeOpenRouterAPIKey),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OpenRouterAPIKeyV1})
}
