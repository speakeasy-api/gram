package enrich

import (
	"context"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// logOperation writes what happened in a log record and how it went: the
// model, the tool, the skill or agent, the words, the outcome and the
// duration.
type logOperation struct{}

func (*logOperation) Name() string {
	return operationEnricherName
}

func (*logOperation) Enrich(_ context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	d := dialect.ForLog(record)
	eventType := stated(d.EventType(record))
	if eventType == dialect.EventTypeUnclassified {
		return nil, nil
	}
	isRequest := eventType == dialect.EventTypeAPIRequest
	isTool := isToolEvent(eventType)
	gateway := isGatewayRecord(record)

	var out []attribute.KeyValue
	if carriesModel(eventType) {
		if key, v, err := d.Model(record); known(key, err) {
			out = append(out, AgentModelKey.String(v))
		}
	}
	if isRequest {
		if key, v, err := d.QuerySource(record); known(key, err) {
			out = append(out, AgentQuerySourceKey.String(v))
		}
	}
	if isRequest || isTool {
		if key, v, err := d.SkillName(record); known(key, err) {
			out = append(out, AgentSkillNameKey.String(v))
		}
		if key, v, err := d.AgentName(record); known(key, err) {
			out = append(out, AgentAgentNameKey.String(v))
		}
		if gateway {
			if v := inboundLogAttributeString(record, string(attr.ToolsetSlugKey)); v != "" {
				out = append(out, AgentMCPServerNameKey.String(v))
			}
			if v := inboundLogAttributeString(record, string(attr.GenAIToolNameKey)); v != "" {
				out = append(out, AgentMCPToolNameKey.String(v))
			}
		} else {
			if key, v, err := d.MCPServerName(record); known(key, err) {
				out = append(out, AgentMCPServerNameKey.String(v))
			}
			if key, v, err := d.MCPToolName(record); known(key, err) {
				out = append(out, AgentMCPToolNameKey.String(v))
			}
		}
	}
	// name is the subject's name, the generic pair to event_id: the tool on
	// tool events. tool_name says the same until a contract migration drops it.
	if isTool {
		if key, v, err := d.ToolName(record); known(key, err) {
			out = append(out, AgentNameKey.String(v), AgentToolNameKey.String(v))
		}
	}
	if carriesText(eventType) {
		if key, v, err := d.Text(record); known(key, err) {
			out = append(out, AgentTextKey.String(capText(v, maxTextBytes)))
		}
	}
	// The gateway's one record is the whole call, so its tool_call says how
	// the call went and how long it took.
	if gateway {
		out = append(out, AgentOutcomeKey.String(gatewayOutcome(record)))
		if v := inboundLogAttributeString(record, string(attr.ErrorMessageKey)); v != "" {
			out = append(out, AgentOutcomeMessageKey.String(v))
		}
		if v, ok := gatewayDurationNano(record); ok {
			out = append(out, AgentDurationNanoKey.Int64(v))
		}
		return out, nil
	}
	if implied := impliedOutcome(eventType); implied != "" {
		out = append(out, AgentOutcomeKey.String(implied))
	} else if statesOutcome(eventType) {
		if key, v, err := d.Outcome(record); known(key, err) {
			out = append(out, AgentOutcomeKey.String(v))
		}
	}
	if carriesOutcomeMessage(eventType) {
		if key, v, err := d.OutcomeMessage(record); known(key, err) {
			out = append(out, AgentOutcomeMessageKey.String(v))
		}
	}
	if carriesDuration(eventType) {
		if key, v, err := d.DurationNano(record); known(key, err) {
			out = append(out, AgentDurationNanoKey.Int64(v))
		}
	}
	return out, nil
}

// isGatewayRecord reports whether the MCP gateway wrote record about a tool call it ran.
func isGatewayRecord(record *otelv1.InboundLogRecord) bool {
	return record.GetScope().GetName() == dialect.GatewayLogScope
}

// gatewayOutcome reads error.type, which the gateway stamps on every call the
// client read as an error.
func gatewayOutcome(record *otelv1.InboundLogRecord) string {
	if inboundLogAttributeString(record, string(attr.ErrorTypeKey)) != "" {
		return dialect.OutcomeError
	}
	return dialect.OutcomeOK
}

// gatewayDurationNano reads the gateway's timing of the call, recorded in seconds.
func gatewayDurationNano(record *otelv1.InboundLogRecord) (int64, bool) {
	for _, kv := range record.GetAttributes() {
		if kv.GetKey() == string(attr.ToolCallDurationKey) && kv.GetValue().HasDoubleValue() {
			return int64(kv.GetValue().GetDoubleValue() * float64(time.Second)), true
		}
	}
	return 0, false
}
