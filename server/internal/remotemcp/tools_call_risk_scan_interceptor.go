package remotemcp

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

type toolsCallRiskScanInterceptor struct {
	evaluator *mcpriskscan.Evaluator
	event     mcpriskscan.Event
}

var _ proxy.ToolsCallRequestInterceptor = (*toolsCallRiskScanInterceptor)(nil)

// NewToolsCallRiskScanInterceptor creates an observation-only request-phase
// interceptor. Decoding guarantees non-nil Params in that phase. Never register
// it in ToolsCallPreForwardInterceptors, where malformed calls can have nil Params.
func NewToolsCallRiskScanInterceptor(evaluator *mcpriskscan.Evaluator, event mcpriskscan.Event) proxy.ToolsCallRequestInterceptor {
	return &toolsCallRiskScanInterceptor{
		evaluator: evaluator,
		event:     event,
	}
}

func (i *toolsCallRiskScanInterceptor) Name() string {
	return "tools-call-risk-scan"
}

func (i *toolsCallRiskScanInterceptor) InterceptToolsCallRequest(ctx context.Context, call *proxy.ToolsCallRequest) error {
	event := i.event
	event.ToolName = call.Params.Name
	i.evaluator.Scan(ctx, mcpriskscan.NewRequest(ctx, event, mcpriskscan.BorrowPayload(call.Params.Arguments)))
	// This adapter cannot reject traffic; Evaluator.Scan has no decision or error result.
	return nil
}
