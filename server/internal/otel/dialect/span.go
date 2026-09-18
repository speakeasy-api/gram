package dialect

import (
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

type SpanDialect interface {
	AppliesTo(span *otelv1.InboundSpan) bool
	InputContent(span *otelv1.InboundSpan) (key string, val genaiconv.InputMessages, err error)
	OutputContent(span *otelv1.InboundSpan) (key string, val genaiconv.OutputMessages, err error)
	SessionID(span *otelv1.InboundSpan) (key string, val string, err error)
	ExternalUserID(span *otelv1.InboundSpan) (key string, val string, err error)
	ExternalUserEmail(span *otelv1.InboundSpan) (key string, val string, err error)
	ResponseID(span *otelv1.InboundSpan) (key string, val string, err error)

	// Producer knowledge. The key is what identified the producer, since the
	// answer is not read from the record.
	Provider(span *otelv1.InboundSpan) (key string, val string, err error)
	Surface(span *otelv1.InboundSpan) (key string, val string, err error)

	// What the record is, in agent vocabulary. Every answer names the
	// attribute it came from; an empty key means the producer did not say.
	EventName(span *otelv1.InboundSpan) (key string, val string, err error)
	EventType(span *otelv1.InboundSpan) (key string, val string, err error)
	SubjectID(span *otelv1.InboundSpan) (key string, val string, err error)
	TurnID(span *otelv1.InboundSpan) (key string, val string, err error)
	Model(span *otelv1.InboundSpan) (key string, val string, err error)
	ToolName(span *otelv1.InboundSpan) (key string, val string, err error)
	Outcome(span *otelv1.InboundSpan) (key string, val string, err error)
	OutcomeMessage(span *otelv1.InboundSpan) (key string, val string, err error)
	Text(span *otelv1.InboundSpan) (key string, val string, err error)
	DurationNano(span *otelv1.InboundSpan) (key string, val int64, err error)
	InputTokens(span *otelv1.InboundSpan) (key string, val int64, err error)
	OutputTokens(span *otelv1.InboundSpan) (key string, val int64, err error)
	CacheReadTokens(span *otelv1.InboundSpan) (key string, val int64, err error)
	CacheWriteTokens(span *otelv1.InboundSpan) (key string, val int64, err error)
	CostUSD(span *otelv1.InboundSpan) (key string, val float64, err error)
	QuerySource(span *otelv1.InboundSpan) (key string, val string, err error)
	SkillName(span *otelv1.InboundSpan) (key string, val string, err error)
	AgentName(span *otelv1.InboundSpan) (key string, val string, err error)
	MCPServerName(span *otelv1.InboundSpan) (key string, val string, err error)
	MCPToolName(span *otelv1.InboundSpan) (key string, val string, err error)
	ExternalOrgID(span *otelv1.InboundSpan) (key string, val string, err error)
}

var dialects = []SpanDialect{
	ClaudeCodeSpan{},
}

func ForSpan(span *otelv1.InboundSpan) SpanDialect {
	if span == nil {
		return NilSpan{}
	}

	for _, e := range dialects {
		if e.AppliesTo(span) {
			return Fallback{Candidates: []SpanDialect{e, SemconvSpan{}}}
		}
	}

	return SemconvSpan{}
}
