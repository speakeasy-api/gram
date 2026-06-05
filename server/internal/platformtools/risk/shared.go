package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
)

// RiskService is the subset of the risk management service that the managed
// assistant's risk tools call. The concrete risk service satisfies it; tools
// pass nil auth tokens because the assistant runtime supplies auth context out
// of band.
type RiskService interface {
	ListRiskPolicies(ctx context.Context, payload *risk.ListRiskPoliciesPayload) (*risk.ListRiskPoliciesResult, error)
	ListRiskResultsForAgent(ctx context.Context, payload *risk.ListRiskResultsForAgentPayload) (*risk.ListRiskResultsForAgentResult, error)
	ListRiskResultsByChat(ctx context.Context, payload *risk.ListRiskResultsByChatPayload) (*risk.ListRiskResultsByChatResult, error)
	GetRiskPolicyStatus(ctx context.Context, payload *risk.GetRiskPolicyStatusPayload) (*types.RiskPolicyStatus, error)
}

func readOnlyToolAnnotations() *types.ToolAnnotations {
	readOnly := true
	destructive := false
	idempotent := true
	openWorld := false
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}

func decodeToolInput(payload io.Reader, dst any) error {
	body, err := io.ReadAll(payload)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func encodeToolResult(wr io.Writer, result any) error {
	if err := json.NewEncoder(wr).Encode(result); err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	return nil
}
