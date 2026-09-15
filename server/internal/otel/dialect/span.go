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
