package remotemcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/speakeasy-api/gram/server/internal/mcpriskscan"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
)

type toolsCallRiskScanInterceptor struct {
	evaluator *mcpriskscan.Evaluator
	event     mcpriskscan.Event
}

type toolsCallRiskScanSubjectKey struct{}

var (
	_ proxy.ToolsCallRequestInterceptor  = (*toolsCallRiskScanInterceptor)(nil)
	_ proxy.ToolsCallResponseInterceptor = (*toolsCallRiskScanInterceptor)(nil)
)

// NewToolsCallRiskScanInterceptor creates request and response enforcement.
// Decoding guarantees non-nil Params in the request phase.
func NewToolsCallRiskScanInterceptor(evaluator *mcpriskscan.Evaluator, event mcpriskscan.Event) *toolsCallRiskScanInterceptor {
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
	subject := mcpriskscan.NewRequest(ctx, event, mcpriskscan.BorrowPayload(call.Params.Arguments))
	if call.UserRequest != nil && call.UserRequest.UserHTTPRequest != nil {
		request := call.UserRequest.UserHTTPRequest
		call.UserRequest.UserHTTPRequest = request.WithContext(context.WithValue(request.Context(), toolsCallRiskScanSubjectKey{}, subject))
	}
	decision := i.evaluator.Scan(ctx, subject)
	return riskScanRejection(decision)
}

func (i *toolsCallRiskScanInterceptor) InterceptToolsCallResponse(ctx context.Context, call *proxy.ToolsCallResponse) error {
	if call.Request == nil || call.Request.UserRequest == nil || call.Request.UserRequest.UserHTTPRequest == nil {
		return nil
	}
	requestSubject, ok := call.Request.UserRequest.UserHTTPRequest.Context().Value(toolsCallRiskScanSubjectKey{}).(mcpriskscan.Subject)
	if !ok {
		return nil
	}

	payload := mcpriskscan.JSONRPCErrorPayload(call.Error)
	if call.Result != nil {
		payload = mcpriskscan.Payload{}
		if call.RemoteMessage != nil {
			if rpcResponse, ok := call.RemoteMessage.Message.(*jsonrpc.Response); ok {
				parsed, err := mcpriskscan.ParseToolResultPayload(rpcResponse.Result)
				if err == nil {
					payload = parsed
				}
			}
		}
	}
	decision := i.evaluator.Scan(ctx, mcpriskscan.NewResponse(requestSubject, payload))
	return riskScanRejection(decision)
}

func riskScanRejection(decision mcpriskscan.Decision) error {
	if decision.Denied() {
		return &proxy.RejectError{
			Code:    proxy.RejectCodeForbidden,
			Message: decision.UserMessage,
			Data:    nil,
		}
	}
	return nil
}
