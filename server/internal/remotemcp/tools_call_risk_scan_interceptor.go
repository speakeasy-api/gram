package remotemcp

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

// toolsCallRiskScanInterceptor is request-phase only: decoding guarantees non-nil
// Params there. Never register it in ToolsCallPreForwardInterceptors, where
// malformed calls can have nil Params.
type toolsCallRiskScanInterceptor struct {
	evaluator mcpriskscan.Evaluator
	event     mcpriskscan.Event
}

var _ proxy.ToolsCallRequestInterceptor = (*toolsCallRiskScanInterceptor)(nil)

func (i *toolsCallRiskScanInterceptor) Name() string {
	return "tools-call-risk-scan"
}

func (i *toolsCallRiskScanInterceptor) InterceptToolsCallRequest(ctx context.Context, call *proxy.ToolsCallRequest) error {
	event := i.event
	event.ToolName = call.Params.Name
	event.Payload = call.Params.Arguments
	i.evaluator.Scan(ctx, event)
	// This adapter cannot reject traffic; Evaluator.Scan has no decision or error result.
	return nil
}
