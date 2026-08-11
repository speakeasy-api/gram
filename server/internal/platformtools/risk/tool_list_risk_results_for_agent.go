package risk

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// A page of findings costs roughly 200 tokens per row and stays in the
// transcript for the rest of the turn, so the agent gets a much tighter budget
// than the dashboard's 200. Anything wider than this is a question for
// platform_get_risk_rule_breakdown, not a bigger page.
const (
	agentResultsDefaultLimit = 25
	agentResultsMaxLimit     = 50
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
	Limit        *int    `json:"limit,omitempty" jsonschema:"Maximum results per page. Defaults to 25. Keep this small: a full page of findings is tens of thousands of tokens and stays in context for the rest of the turn. To characterize a large finding set, use platform_get_risk_rule_breakdown instead of paginating."`
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
			core.WithPropertyNumberRange("limit", 1, agentResultsMaxLimit),
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

	// The service defaults an omitted limit to its own (larger) page size, so
	// the agent's default is applied here rather than left unset. The schema
	// range is advisory — DecodeInput is a plain unmarshal — so the bound is
	// enforced here too.
	limitValue := agentResultsDefaultLimit
	if input.Limit != nil {
		limitValue = min(max(*input.Limit, 1), agentResultsMaxLimit)
	}
	limit := &limitValue

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
		Limit:            limit,
	})
	if err != nil {
		return fmt.Errorf("list risk results for agent: %w", err)
	}

	return core.EncodeResult(wr, result)
}
