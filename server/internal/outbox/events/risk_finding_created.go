package events

import (
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/outbox"
)

var RiskFindingCreated = outbox.NewEventDef[RiskFindingCreatedPayload](
	"risk_finding.created",
	"A potential risk was detected in a LLM message or tool call",
)

type RiskFindingCreatedPayload struct {
	ID                uuid.UUID `json:"id"`
	ProjectID         uuid.UUID `json:"project_id"`
	OrganizationID    string    `json:"organization_id"`
	RiskPolicyID      uuid.UUID `json:"risk_policy_id"`
	RiskPolicyVersion int64     `json:"risk_policy_version"`
	ChatMessageID     uuid.UUID `json:"chat_message_id"`
	RuleID            string    `json:"rule_id"`
	Description       string    `json:"description"`
	Confidence        float64   `json:"confidence"`
	Tags              []string  `json:"tags"`
	CreatedAt         time.Time `json:"created_at"`
}
