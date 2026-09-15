package remotemcp

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/riskscan"
)

type toolsCallRiskScanInterceptor struct {
	hook  riskscan.Hook
	event riskscan.Event
}

var _ proxy.ToolsCallRequestInterceptor = (*toolsCallRiskScanInterceptor)(nil)

func (i *toolsCallRiskScanInterceptor) Name() string {
	return "tools-call-risk-scan"
}

func (i *toolsCallRiskScanInterceptor) InterceptToolsCallRequest(ctx context.Context, call *proxy.ToolsCallRequest) error {
	event := i.event
	event.ToolName = call.Params.Name
	i.hook.Scan(ctx, event)
	return nil
}
