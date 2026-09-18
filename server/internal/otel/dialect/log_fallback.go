package dialect

import (
	"errors"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/genaiconv"
)

type LogFallback struct {
	Candidates []LogDialect
}

func (f LogFallback) AppliesTo(record *otelv1.InboundLogRecord) bool {
	for _, candidate := range f.Candidates {
		if candidate.AppliesTo(record) {
			return true
		}
	}
	return false
}

func firstLogFallback[V any](f LogFallback, record *otelv1.InboundLogRecord, callback func(LogDialect, *otelv1.InboundLogRecord) (string, V, error)) (string, V, error) {
	var zero V
	var errs []error
	for _, candidate := range f.Candidates {
		key, value, err := callback(candidate, record)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if key != "" {
			return key, value, nil
		}
	}
	return "", zero, errors.Join(errs...)
}

func (f LogFallback) InputContent(record *otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, genaiconv.InputMessages, error) {
		return d.InputContent(r)
	})
}

func (f LogFallback) OutputContent(record *otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, genaiconv.OutputMessages, error) {
		return d.OutputContent(r)
	})
}

func (f LogFallback) SessionID(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) {
		return d.SessionID(r)
	})
}

func (f LogFallback) ExternalUserID(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) {
		return d.ExternalUserID(r)
	})
}

func (f LogFallback) ExternalUserEmail(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) {
		return d.ExternalUserEmail(r)
	})
}

func (f LogFallback) ResponseID(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) {
		return d.ResponseID(r)
	})
}

// The agent-vocabulary accessors, each taking the first candidate that
// answered, exactly like the identity accessors above.
func (f LogFallback) Provider(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.Provider(r) })
}

func (f LogFallback) Surface(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.Surface(r) })
}

func (f LogFallback) EventName(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.EventName(r) })
}

func (f LogFallback) EventType(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.EventType(r) })
}

func (f LogFallback) SubjectID(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.SubjectID(r) })
}

func (f LogFallback) TurnID(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.TurnID(r) })
}

func (f LogFallback) Model(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.Model(r) })
}

func (f LogFallback) ToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.ToolName(r) })
}

func (f LogFallback) Outcome(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.Outcome(r) })
}

func (f LogFallback) OutcomeMessage(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.OutcomeMessage(r) })
}

func (f LogFallback) Text(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.Text(r) })
}

func (f LogFallback) QuerySource(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.QuerySource(r) })
}

func (f LogFallback) SkillName(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.SkillName(r) })
}

func (f LogFallback) AgentName(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.AgentName(r) })
}

func (f LogFallback) MCPServerName(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.MCPServerName(r) })
}

func (f LogFallback) MCPToolName(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.MCPToolName(r) })
}

func (f LogFallback) ExternalOrgID(record *otelv1.InboundLogRecord) (string, string, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, string, error) { return d.ExternalOrgID(r) })
}

func (f LogFallback) DurationNano(record *otelv1.InboundLogRecord) (string, int64, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, int64, error) { return d.DurationNano(r) })
}

func (f LogFallback) InputTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, int64, error) { return d.InputTokens(r) })
}

func (f LogFallback) OutputTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, int64, error) { return d.OutputTokens(r) })
}

func (f LogFallback) CacheReadTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, int64, error) { return d.CacheReadTokens(r) })
}

func (f LogFallback) CacheWriteTokens(record *otelv1.InboundLogRecord) (string, int64, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, int64, error) { return d.CacheWriteTokens(r) })
}

func (f LogFallback) CostUSD(record *otelv1.InboundLogRecord) (string, float64, error) {
	return firstLogFallback(f, record, func(d LogDialect, r *otelv1.InboundLogRecord) (string, float64, error) { return d.CostUSD(r) })
}
