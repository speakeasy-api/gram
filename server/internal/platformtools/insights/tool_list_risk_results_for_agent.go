package insights

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type ListRiskResultsForAgent struct {
	provider func() RiskService
}

type listRiskResultsForAgentInput struct {
	PolicyID    *string `json:"policy_id,omitempty" jsonschema:"Optional policy ID to filter by."`
	ChatID      *string `json:"chat_id,omitempty" jsonschema:"Optional chat ID to filter by."`
	Category    *string `json:"category,omitempty" jsonschema:"Optional rule category key to filter by (e.g. secrets, pii, financial)."`
	RuleID      *string `json:"rule_id,omitempty" jsonschema:"Optional rule identifier substring to filter by (case-insensitive)."`
	UserID      *string `json:"user_id,omitempty" jsonschema:"Optional user identifier substring (matched against the chat's external user id)."`
	UniqueMatch *bool   `json:"unique_match,omitempty" jsonschema:"Collapse results to one row per (policy_id, rule_id, match), keeping the most recent occurrence."`
	From        *string `json:"from,omitempty" jsonschema:"Only findings on messages created at or after this ISO 8601 timestamp."`
	To          *string `json:"to,omitempty" jsonschema:"Only findings on messages created strictly before this ISO 8601 timestamp."`
	Cursor      *string `json:"cursor,omitempty" jsonschema:"Cursor for pagination."`
	Limit       *int    `json:"limit,omitempty" jsonschema:"Maximum number of results per page."`
}

func NewListRiskResultsForAgentTool(provider func() RiskService) *ListRiskResultsForAgent {
	return &ListRiskResultsForAgent{provider: provider}
}

func (s *ListRiskResultsForAgent) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "insights",
		HandlerName: "list_risk_results_for_agent",
		Name:        "platform_list_risk_results_for_agent",
		Description: "List individual risk/safety findings (secrets, PII, prompt injection, etc.) detected across the project's chats. Match values are redacted. Requires organization admin access.",
		InputSchema: core.BuildInputSchema[listRiskResultsForAgentInput](
			core.WithPropertyFormat("from", "date-time"),
			core.WithPropertyFormat("to", "date-time"),
		),
		Variables:   nil,
		Annotations: readOnlyToolAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *ListRiskResultsForAgent) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	svc := s.provider()
	if svc == nil {
		return fmt.Errorf("risk service not configured")
	}

	input := listRiskResultsForAgentInput{
		PolicyID:    nil,
		ChatID:      nil,
		Category:    nil,
		RuleID:      nil,
		UserID:      nil,
		UniqueMatch: nil,
		From:        nil,
		To:          nil,
		Cursor:      nil,
		Limit:       nil,
	}
	if err := decodeToolInput(payload, &input); err != nil {
		return err
	}

	result, err := svc.ListRiskResultsForAgent(ctx, &risk.ListRiskResultsForAgentPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		PolicyID:         input.PolicyID,
		ChatID:           input.ChatID,
		Category:         input.Category,
		RuleID:           input.RuleID,
		UserID:           input.UserID,
		UniqueMatch:      input.UniqueMatch,
		From:             input.From,
		To:               input.To,
		Cursor:           input.Cursor,
		Limit:            input.Limit,
	})
	if err != nil {
		return fmt.Errorf("list risk results for agent: %w", err)
	}

	return encodeToolResult(wr, result)
}
