package enrich

import (
	"context"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// spanOperation is logOperation for spans.
type spanOperation struct {
	instruments *Instruments
}

func (*spanOperation) Name() string {
	return operationEnricherName
}

func (e *spanOperation) Enrich(ctx context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
	d := dialect.ForSpan(span)
	eventType := stated(d.EventType(span))
	if eventType == dialect.EventTypeUnclassified {
		return nil, nil
	}
	surface := func() string { return missingLabel(d.Surface(span)) }
	isRequest := eventType == dialect.EventTypeAPIRequest
	isTool := isToolEvent(eventType)

	var out []attribute.KeyValue
	if carriesModel(eventType) {
		if key, v, err := d.Model(span); known(key, err) {
			out = append(out, AgentModelKey.String(v))
		}
	}
	if isRequest {
		if key, v, err := d.QuerySource(span); known(key, err) {
			out = append(out, AgentQuerySourceKey.String(v))
		}
	}
	if isRequest || isTool {
		if key, v, err := d.SkillName(span); known(key, err) {
			out = append(out, AgentSkillNameKey.String(v))
		}
		if key, v, err := d.AgentName(span); known(key, err) {
			out = append(out, AgentAgentNameKey.String(v))
		}
		if key, v, err := d.MCPServerName(span); known(key, err) {
			out = append(out, AgentMCPServerNameKey.String(v))
		}
		if key, v, err := d.MCPToolName(span); known(key, err) {
			out = append(out, AgentMCPToolNameKey.String(v))
		}
	}
	if isTool {
		if key, v, err := d.ToolName(span); known(key, err) {
			out = append(out, AgentNameKey.String(v), AgentToolNameKey.String(v))
		}
	}
	if carriesText(eventType) {
		if key, v, err := d.Text(span); known(key, err) {
			out = append(out, AgentTextKey.String(capText(ctx, e.instruments, surface, eventType, v, maxTextBytes)))
		}
	}
	if implied := impliedOutcome(eventType); implied != "" {
		out = append(out, AgentOutcomeKey.String(implied))
	} else if statesOutcome(eventType) {
		if key, v, err := d.Outcome(span); known(key, err) {
			out = append(out, AgentOutcomeKey.String(v))
		}
	}
	if carriesOutcomeMessage(eventType) {
		if key, v, err := d.OutcomeMessage(span); known(key, err) {
			out = append(out, AgentOutcomeMessageKey.String(v))
		}
	}
	if carriesDuration(eventType) {
		if key, v, err := d.DurationNano(span); known(key, err) {
			out = append(out, AgentDurationNanoKey.Int64(v))
		}
	}

	countMissing(ctx, e.instruments, surface, eventType, operationExpectations, out)
	return out, nil
}
