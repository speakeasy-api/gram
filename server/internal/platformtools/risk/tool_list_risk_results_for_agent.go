package risk

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type ListRiskResultsForAgent struct {
	risk RiskService
}

type listRiskResultsForAgentInput struct {
	PolicyID     *string `json:"policy_id,omitempty" jsonschema:"Restrict to results produced by this policy ID."`
	ChatID       *string `json:"chat_id,omitempty" jsonschema:"Restrict to results from this chat ID."`
	Category     *string `json:"category,omitempty" jsonschema:"Rule category key (e.g. secrets, pii, financial)."`
	RuleID       *string `json:"rule_id,omitempty" jsonschema:"Case-insensitive substring of the rule identifier."`
	UserID       *string `json:"user_id,omitempty" jsonschema:"Case-insensitive substring matched against the chat's external user ID."`
	UniqueMatch  *bool   `json:"unique_match,omitempty" jsonschema:"Collapse to one row per (policy_id, rule_id, match), keeping the most recent occurrence."`
	NonAssistant *bool   `json:"non_assistant,omitempty" jsonschema:"Only return findings from chats that are not linked to an assistant. Useful for surfacing events missing user attribution."`
	AssistantID  *string `json:"assistant_id,omitempty" jsonschema:"Only return findings from chats linked to this assistant ID."`
	From         *string `json:"from,omitempty" jsonschema:"Filter to messages created at or after this ISO 8601 timestamp."`
	To           *string `json:"to,omitempty" jsonschema:"Filter to messages created strictly before this ISO 8601 timestamp."`
	Cursor       *string `json:"cursor,omitempty" jsonschema:"Cursor for pagination."`
	Limit        *int    `json:"limit,omitempty" jsonschema:"Maximum results per page."`
}

func NewListRiskResultsForAgentTool(riskSvc RiskService) *ListRiskResultsForAgent {
	return &ListRiskResultsForAgent{risk: riskSvc}
}

func (s *ListRiskResultsForAgent) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "risk",
		HandlerName: "list_risk_results_for_agent",
		Name:        "platform_list_risk_results_for_agent",
		Description: "List risk findings for the current project with secret content redacted to a length+sha256-prefix fingerprint so it never reaches the model context. Same filters and pagination as listRiskResults.",
		InputSchema: core.BuildInputSchema[listRiskResultsForAgentInput](
			core.WithPropertyFormat("policy_id", "uuid"),
			core.WithPropertyFormat("chat_id", "uuid"),
			core.WithPropertyFormat("assistant_id", "uuid"),
			core.WithPropertyFormat("from", "date-time"),
			core.WithPropertyFormat("to", "date-time"),
			core.WithPropertyNumberRange("limit", 1, 200),
		),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *ListRiskResultsForAgent) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.risk == nil {
		return fmt.Errorf("risk service not configured")
	}

	input := listRiskResultsForAgentInput{
		PolicyID:     nil,
		ChatID:       nil,
		Category:     nil,
		RuleID:       nil,
		UserID:       nil,
		UniqueMatch:  nil,
		NonAssistant: nil,
		AssistantID:  nil,
		From:         nil,
		To:           nil,
		Cursor:       nil,
		Limit:        nil,
	}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}

	result, err := s.risk.ListRiskResultsForAgent(ctx, &risk.ListRiskResultsForAgentPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		PolicyID:         input.PolicyID,
		ChatID:           input.ChatID,
		Category:         input.Category,
		RuleID:           input.RuleID,
		UserID:           input.UserID,
		UniqueMatch:      input.UniqueMatch,
		NonAssistant:     input.NonAssistant,
		AssistantID:      input.AssistantID,
		From:             input.From,
		To:               input.To,
		Cursor:           input.Cursor,
		Limit:            input.Limit,
	})
	if err != nil {
		return fmt.Errorf("list risk results for agent: %w", err)
	}

	return core.EncodeResult(wr, result)
}
