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
	observer mcpriskscan.Observer
	event    mcpriskscan.Event
}

var _ proxy.ToolsCallRequestInterceptor = (*toolsCallRiskScanInterceptor)(nil)

func (i *toolsCallRiskScanInterceptor) Name() string {
	return "tools-call-risk-scan"
}

func (i *toolsCallRiskScanInterceptor) InterceptToolsCallRequest(ctx context.Context, call *proxy.ToolsCallRequest) error {
	event := i.event
	event.ToolName = call.Params.Name
	event.Payload = call.Params.Arguments
	i.observer.Scan(ctx, event)
	// Observation cannot reject traffic; Observer.Scan deliberately has no error result.
	return nil
}
