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
	ActionOpenRouterAPIKeyDisable     Action = "openrouter-key:disable"
	ActionOpenRouterAPIKeyEnable      Action = "openrouter-key:enable"
	ActionOpenRouterAPIKeySetSpendCap Action = "openrouter-key:set_spend_cap"
)

// openRouterAPIKeyMetadata carries the key type so consumers can tell the
// chat and internal key series apart without parsing the subject id. Key
// material never appears in audit entries.
type openRouterAPIKeyMetadata struct {
	KeyType     string `json:"key_type"`
	OperationID string `json:"operation_id,omitempty"`
}

// OpenRouterAPIKeySpendCapSnapshot captures the customer-facing monthly cap
// without including any key material.
type OpenRouterAPIKeySpendCapSnapshot struct {
	MonthlyCredits int64 `json:"monthly_credits"`
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

type LogOpenRouterAPIKeySetSpendCapEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	OpenRouterAPIKeyURN urn.OpenRouterAPIKey

	KeyType string
	// OperationIdentifier deduplicates a Temporal activity retry after its first audit
	// transaction committed but before the activity result was acknowledged.
	OperationIdentifier string

	OpenRouterAPIKeySnapshotBefore *OpenRouterAPIKeySpendCapSnapshot
	OpenRouterAPIKeySnapshotAfter  *OpenRouterAPIKeySpendCapSnapshot
}

// LogOpenRouterAPIKeySetSpendCap records an administrator-requested inference
// cap change. The workflow applies the after value to the selected provider
// key and mirrors it locally.
func (l *Logger) LogOpenRouterAPIKeySetSpendCap(ctx context.Context, dbtx repo.DBTX, event LogOpenRouterAPIKeySetSpendCapEvent) error {
	action := ActionOpenRouterAPIKeySetSpendCap
	metadata, err := json.Marshal(openRouterAPIKeyMetadata{KeyType: event.KeyType, OperationID: event.OperationIdentifier})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	beforeSnapshot, err := marshalAuditPayload(event.OpenRouterAPIKeySnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.OpenRouterAPIKeySnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	subjectDisplayName := "OpenRouter inference cap"
	switch event.KeyType {
	case "chat":
		subjectDisplayName = "Other inference cap"
	case "internal":
		subjectDisplayName = "Security inference cap"
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.OpenRouterAPIKeyURN.ID,
		SubjectType:        string(subjectTypeOpenRouterAPIKey),
		SubjectDisplayName: conv.ToPGTextEmpty(subjectDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.OpenRouterAPIKeyV1})
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
	metadata, err := json.Marshal(openRouterAPIKeyMetadata{KeyType: fields.KeyType, OperationID: ""})
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
