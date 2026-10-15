package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
)

// GatewayLogScope is the scope the MCP gateway emits its tool call records under.
const GatewayLogScope = "github.com/speakeasy-api/gram/server/internal/mcp"

const (
	gatewayMCPClientNameKey    = "gram.mcp.client.name"
	gatewayToolsetSlugKey      = "gram.toolset.slug"
	gatewayToolNameKey         = "gen_ai.tool.name"
	gatewayErrorTypeKey        = "error.type"
	gatewayErrorMessageKey     = "error.message"
	gatewayToolCallDurationKey = "gram.tool_call.duration"
)

// GatewayLog reads the semconv execute_tool record the MCP gateway emits
// after each hosted tool call returns. The record describes the finished
// call, so it is a tool_call_result with an outcome and a duration.
type GatewayLog struct {
	SemconvLog
}

func (GatewayLog) AppliesTo(record *otelv1.InboundLogRecord) bool {
	return record.GetScope().GetName() == GatewayLogScope
}

func (g GatewayLog) EventType(record *otelv1.InboundLogRecord) (string, string, error) {
	key, eventType, err := g.SemconvLog.EventType(record)
	if eventType == EventTypeToolCall {
		eventType = EventTypeToolCallResult
	}
	return key, eventType, err
}

// Surface is whatever MCP client made the call.
func (GatewayLog) Surface(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, gatewayMCPClientNameKey)
	return key, value, nil
}

func (GatewayLog) MCPServerName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, gatewayToolsetSlugKey)
	return key, value, nil
}

func (GatewayLog) MCPToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, gatewayToolNameKey)
	return key, value, nil
}

// Outcome reads error.type, which the gateway stamps on every call the
// client read as an error; every other call went fine.
func (GatewayLog) Outcome(record *otelv1.InboundLogRecord) (string, string, error) {
	if key, _ := getOneLogAttr(record, gatewayErrorTypeKey); key != "" {
		return key, OutcomeError, nil
	}
	return scopeNameKey, OutcomeOK, nil
}

// OutcomeMessage is the error the gateway told the client; a tool's own
// error document is the result, not a message about it.
func (GatewayLog) OutcomeMessage(record *otelv1.InboundLogRecord) (string, string, error) {
	key, value := getOneLogAttr(record, gatewayErrorMessageKey)
	return key, value, nil
}

// DurationNano is the gateway's timing of the call, recorded in seconds.
func (GatewayLog) DurationNano(record *otelv1.InboundLogRecord) (string, int64, error) {
	key, seconds := getOneLogFloat64(record, gatewayToolCallDurationKey)
	if key == "" {
		return "", 0, nil
	}
	return key, int64(seconds * 1e9), nil
}
