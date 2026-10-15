package enrich

import (
	"context"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/toolref"
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
	eventType := logEventType(d, record)
	if eventType == dialect.EventTypeUnclassified {
		return nil, nil
	}
	isRequest := eventType == dialect.EventTypeAPIRequest
	isTool := isToolEvent(eventType)
	hooks := isHooksRecord(record)

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
		switch {
		case hooks:
			if v := inboundLogAttributeString(record, string(attr.ToolCallSourceKey)); v != "" {
				out = append(out, AgentMCPServerNameKey.String(v))
			}
			if v := hooksMCPToolName(record); v != "" {
				out = append(out, AgentMCPToolNameKey.String(v))
			}
		default:
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
		if hooks {
			if v := inboundLogAttributeString(record, string(attr.ToolNameKey)); v != "" {
				out = append(out, AgentNameKey.String(v), AgentToolNameKey.String(v))
			}
		} else if key, v, err := d.ToolName(record); known(key, err) {
			out = append(out, AgentNameKey.String(v), AgentToolNameKey.String(v))
		}
	}
	if carriesText(eventType) {
		if hooks {
			// A permission request's words are Speakeasy's verdict; a prompt's
			// go to chat, not telemetry.
			if v := inboundLogAttributeString(record, string(attr.HookDecisionKey)); v != "" && eventType == dialect.EventTypeToolDecision {
				out = append(out, AgentTextKey.String(v))
			}
		} else if key, v, err := d.Text(record); known(key, err) {
			out = append(out, AgentTextKey.String(capText(v, maxTextBytes)))
		}
	}
	if hooks {
		if statesOutcome(eventType) {
			if v := hooksOutcome(record, stated(d.EventName(record))); v != "" {
				out = append(out, AgentOutcomeKey.String(v))
			}
		}
		if carriesOutcomeMessage(eventType) {
			if v := inboundLogAttributeString(record, string(attr.HookErrorKey)); v != "" {
				out = append(out, AgentOutcomeMessageKey.String(v))
			}
		}
		if v, ok := toolCallDurationNano(record); ok && carriesDuration(eventType) {
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

// isHooksRecord reports whether the hooks ingest endpoint wrote record for a
// hook event an agent delivered.
func isHooksRecord(record *otelv1.InboundLogRecord) bool {
	return record.GetScope().GetName() == dialect.HooksLogScope
}

// hooksOutcome is what a hook event says about the call: a failure did not
// run, a result carrying an error failed, and a permission request
// Speakeasy blocked was rejected.
func hooksOutcome(record *otelv1.InboundLogRecord, hookEvent string) string {
	switch hookEvent {
	case "PostToolUseFailure":
		return dialect.OutcomeError
	case "PostToolUse", "AfterMCPExecution":
		if inboundLogAttributeString(record, string(attr.HookErrorKey)) != "" {
			return dialect.OutcomeError
		}
		return dialect.OutcomeOK
	case "PermissionRequest":
		if inboundLogAttributeString(record, string(attr.HookBlockReasonKey)) != "" {
			return dialect.OutcomeRejected
		}
		return ""
	default:
		return ""
	}
}

// hooksMCPToolName is the function behind a Claude-style
// mcp__server__function tool name, or the tool itself when the row names an
// MCP server for an unprefixed tool, as Cursor's MCP hooks do.
func hooksMCPToolName(record *otelv1.InboundLogRecord) string {
	name := inboundLogAttributeString(record, string(attr.ToolNameKey))
	switch {
	case name == "":
		return ""
	case toolref.IsMCPToolName(name):
		return toolref.MCPFunctionOf(name)
	case inboundLogAttributeString(record, string(attr.ToolCallSourceKey)) != "":
		return name
	default:
		return ""
	}
}

// toolCallDurationNano reads how long a tool call took, recorded in seconds.
func toolCallDurationNano(record *otelv1.InboundLogRecord) (int64, bool) {
	for _, kv := range record.GetAttributes() {
		if kv.GetKey() == string(attr.ToolCallDurationKey) && kv.GetValue().HasDoubleValue() {
			return int64(kv.GetValue().GetDoubleValue() * float64(time.Second)), true
		}
	}
	return 0, false
}
