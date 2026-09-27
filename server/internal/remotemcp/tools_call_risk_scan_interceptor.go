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

// NewToolsCallRiskScanInterceptor creates a request-phase enforcement
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
	decision := i.evaluator.Scan(ctx, mcpriskscan.NewRequest(ctx, event, mcpriskscan.BorrowPayload(call.Params.Arguments)))
	if decision.Denied() {
		return &proxy.RejectError{
			Code:    proxy.RejectCodeForbidden,
			Message: decision.UserMessage,
			Data:    nil,
		}
	}
	return nil
}
