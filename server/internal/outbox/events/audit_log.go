package events

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/outbox"
)

// AuditLogCreated is the event emitted after every audit log entry is written.
var AuditLogCreated = outbox.NewEventDef[AuditLogCreatedPayload](
	"audit_log.created",
	"An audit log entry was recorded",
)

// AuditLogCreatedPayload is the webhook payload for audit_log.created events.
type AuditLogCreatedPayload struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID string    `json:"organization_id"`

	ActorID     string `json:"actor_id"`
	ActorType   string `json:"actor_type"`
	Action      string `json:"action"`
	SubjectID   string `json:"subject_id"`
	SubjectType string `json:"subject_type"`

	ProjectID          uuid.NullUUID   `json:"project_id,omitzero"`
	ActorDisplayName   string          `json:"actor_display_name,omitzero"`
	ActorSlug          string          `json:"actor_slug,omitzero"`
	SubjectDisplayName string          `json:"subject_display_name,omitzero"`
	SubjectSlug        string          `json:"subject_slug,omitzero"`
	BeforeSnapshot     json.RawMessage `json:"before_snapshot,omitempty"`
	AfterSnapshot      json.RawMessage `json:"after_snapshot,omitempty"`
	Metadata           json.RawMessage `json:"metadata,omitempty"`
}
