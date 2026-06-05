package risk

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type ListRiskResultsByChat struct {
	risk RiskService
}

type listRiskResultsByChatInput struct {
	Cursor *string `json:"cursor,omitempty" jsonschema:"Cursor for pagination."`
	Limit  *int    `json:"limit,omitempty" jsonschema:"Maximum results per page."`
}

func NewListRiskResultsByChatTool(riskSvc RiskService) *ListRiskResultsByChat {
	return &ListRiskResultsByChat{risk: riskSvc}
}

func (s *ListRiskResultsByChat) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "risk",
		HandlerName: "list_risk_results_by_chat",
		Name:        "platform_list_risk_results_by_chat",
		Description: "List risk findings grouped by chat session for the current project.",
		InputSchema: core.BuildInputSchema[listRiskResultsByChatInput](
			core.WithPropertyNumberRange("limit", 1, 200),
		),
		Variables:   nil,
		Annotations: readOnlyToolAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *ListRiskResultsByChat) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if s.risk == nil {
		return fmt.Errorf("risk service not configured")
	}

	input := listRiskResultsByChatInput{Cursor: nil, Limit: nil}
	if err := decodeToolInput(payload, &input); err != nil {
		return err
	}

	result, err := s.risk.ListRiskResultsByChat(ctx, &risk.ListRiskResultsByChatPayload{
		ApikeyToken:      nil,
		SessionToken:     nil,
		ProjectSlugInput: nil,
		Cursor:           input.Cursor,
		Limit:            input.Limit,
	})
	if err != nil {
		return fmt.Errorf("list risk results by chat: %w", err)
	}

	return encodeToolResult(wr, result)
}
