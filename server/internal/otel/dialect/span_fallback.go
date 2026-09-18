package dialect

import (
	"errors"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

type Fallback struct {
	Candidates []SpanDialect
}

func (f Fallback) AppliesTo(span *otelv1.InboundSpan) bool {
	for _, d := range f.Candidates {
		if d.AppliesTo(span) {
			return true
		}
	}

	return false
}

func firstFallback[V any](f Fallback, span *otelv1.InboundSpan, cb func(d SpanDialect, span *otelv1.InboundSpan) (string, V, error)) (key string, val V, err error) {
	var zero V
	var errs error

	for _, d := range f.Candidates {
		key, val, err = cb(d, span)

		errs = errors.Join(errs, err)

		if err == nil && key != "" {
			return
		}
	}

	if errs != nil {
		return "", zero, errs
	}

	return
}

func (f Fallback) SessionID(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, span *otelv1.InboundSpan) (string, string, error) {
		return d.SessionID(span)
	})
}

func (f Fallback) InputContent(span *otelv1.InboundSpan) (key string, val genaiconv.InputMessages, err error) {
	return firstFallback(f, span, func(d SpanDialect, span *otelv1.InboundSpan) (string, genaiconv.InputMessages, error) {
		return d.InputContent(span)
	})
}

func (f Fallback) OutputContent(span *otelv1.InboundSpan) (key string, val genaiconv.OutputMessages, err error) {
	return firstFallback(f, span, func(d SpanDialect, span *otelv1.InboundSpan) (string, genaiconv.OutputMessages, error) {
		return d.OutputContent(span)
	})
}

func (f Fallback) ExternalUserEmail(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, span *otelv1.InboundSpan) (string, string, error) {
		return d.ExternalUserEmail(span)
	})
}

func (f Fallback) ExternalUserID(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, span *otelv1.InboundSpan) (string, string, error) {
		return d.ExternalUserID(span)
	})
}

func (f Fallback) ResponseID(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, span *otelv1.InboundSpan) (string, string, error) {
		return d.ResponseID(span)
	})
}

// The agent-vocabulary accessors, each taking the first candidate that
// answered, exactly like the identity accessors above.
func (f Fallback) Provider(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.Provider(r) })
}

func (f Fallback) Surface(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.Surface(r) })
}

func (f Fallback) EventName(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.EventName(r) })
}

func (f Fallback) EventType(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.EventType(r) })
}

func (f Fallback) SubjectID(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.SubjectID(r) })
}

func (f Fallback) TurnID(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.TurnID(r) })
}

func (f Fallback) Model(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.Model(r) })
}

func (f Fallback) ToolName(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.ToolName(r) })
}

func (f Fallback) Outcome(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.Outcome(r) })
}

func (f Fallback) OutcomeMessage(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.OutcomeMessage(r) })
}

func (f Fallback) Text(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.Text(r) })
}

func (f Fallback) QuerySource(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.QuerySource(r) })
}

func (f Fallback) SkillName(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.SkillName(r) })
}

func (f Fallback) AgentName(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.AgentName(r) })
}

func (f Fallback) MCPServerName(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.MCPServerName(r) })
}

func (f Fallback) MCPToolName(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.MCPToolName(r) })
}

func (f Fallback) ExternalOrgID(span *otelv1.InboundSpan) (string, string, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, string, error) { return d.ExternalOrgID(r) })
}

func (f Fallback) DurationNano(span *otelv1.InboundSpan) (string, int64, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, int64, error) { return d.DurationNano(r) })
}

func (f Fallback) InputTokens(span *otelv1.InboundSpan) (string, int64, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, int64, error) { return d.InputTokens(r) })
}

func (f Fallback) OutputTokens(span *otelv1.InboundSpan) (string, int64, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, int64, error) { return d.OutputTokens(r) })
}

func (f Fallback) CacheReadTokens(span *otelv1.InboundSpan) (string, int64, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, int64, error) { return d.CacheReadTokens(r) })
}

func (f Fallback) CacheWriteTokens(span *otelv1.InboundSpan) (string, int64, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, int64, error) { return d.CacheWriteTokens(r) })
}

func (f Fallback) CostUSD(span *otelv1.InboundSpan) (string, float64, error) {
	return firstFallback(f, span, func(d SpanDialect, r *otelv1.InboundSpan) (string, float64, error) { return d.CostUSD(r) })
}
