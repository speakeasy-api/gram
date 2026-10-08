package enrich

import (
	"context"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// logOperation writes what happened in a log record and how it went: the
// model, the tool, the skill or agent, the words, the outcome and the
// duration.
type logOperation struct {
	instruments *Instruments
}

func (*logOperation) Name() string {
	return operationEnricherName
}

func (e *logOperation) Enrich(ctx context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	d := dialect.ForLog(record)
	eventType := stated(d.EventType(record))
	if eventType == dialect.EventTypeUnclassified {
		return nil, nil
	}
	surface := func() string { return missingLabel(d.Surface(record)) }
	isRequest := eventType == dialect.EventTypeAPIRequest
	isTool := isToolEvent(eventType)

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
		if key, v, err := d.MCPServerName(record); known(key, err) {
			out = append(out, AgentMCPServerNameKey.String(v))
		}
		if key, v, err := d.MCPToolName(record); known(key, err) {
			out = append(out, AgentMCPToolNameKey.String(v))
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
			out = append(out, AgentTextKey.String(capText(ctx, e.instruments, surface, eventType, v, maxTextBytes)))
		}
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

	countMissing(ctx, e.instruments, surface, eventType, operationExpectations, out)
	return out, nil
}
