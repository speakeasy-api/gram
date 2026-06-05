package insights

import (
	"context"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

type ListRiskResultsByChat struct {
	provider func() RiskService
}

type listRiskResultsByChatInput struct {
	Cursor *string `json:"cursor,omitempty" jsonschema:"Cursor for pagination."`
	Limit  *int    `json:"limit,omitempty" jsonschema:"Maximum number of results per page."`
}

func NewListRiskResultsByChatTool(provider func() RiskService) *ListRiskResultsByChat {
	return &ListRiskResultsByChat{provider: provider}
}

func (s *ListRiskResultsByChat) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "insights",
		HandlerName: "list_risk_results_by_chat",
		Name:        "platform_list_risk_results_by_chat",
		Description: "List per-chat risk rollups (findings count and latest detection) across the project. Requires organization admin access.",
		InputSchema: core.BuildInputSchema[listRiskResultsByChatInput](),
		Variables:   nil,
		Annotations: readOnlyToolAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *ListRiskResultsByChat) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	svc := s.provider()
	if svc == nil {
		return fmt.Errorf("risk service not configured")
	}

	input := listRiskResultsByChatInput{Cursor: nil, Limit: nil}
	if err := decodeToolInput(payload, &input); err != nil {
		return err
	}

	result, err := svc.ListRiskResultsByChat(ctx, &risk.ListRiskResultsByChatPayload{
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
