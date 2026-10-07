package enrich

import (
	"context"
	"errors"
	"fmt"
	"strings"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/agentsurface"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// A column enricher fills one agent_events column in the transform stage.
// It holds a table from event type to a getter: how the value is read for
// that type, a question the provider dialect answers or a constant the type
// implies. The value lands on the record as a canonical
// speakeasy.agent.<column> attribute, next to the producer's original
// attributes, and the agent_events writer copies it from there. The file
// that declares a column's table is the documentation of that column.
//
// Each table entry carries a requirement level, after OpenTelemetry's
// attribute requirement levels
// (https://opentelemetry.io/docs/specs/semconv/general/attribute-requirement-level/):
//
//	Required                a bare entry                     absence is counted
//	Conditionally Required  conditionallyRequired(g, when)   absence is counted when the condition holds
//	Recommended             recommended(g)                   absence is not counted
//	Opt-In                  optIn(g)                         absence is not counted
//
// A gap is counted at Required, and at Conditionally Required when the
// condition holds, never at Recommended or Opt-In. A stated value is written
// at every level.
//
// The rules every table follows:
//
//   - An event type absent from the table means the column is never set for
//     that type. There is no forbidden list: a tool result never gets tokens
//     because the tokens table does not name tool results.
//   - A type in the table whose provider stated nothing writes nothing. At
//     Required it is counted by source, event type and column; absent is
//     never an error, and never a guess.
//   - An unclassified record matches no table, so no column enricher applies
//     to it. It still lands with its payload.

// columnValue is what a column enricher can write: the agent_events columns
// the enrichers fill are strings, integers and floating-point numbers.
type columnValue interface {
	string | int64 | float64
}

// getter reads one column's value out of a record for one event type, in the
// sense OpenTelemetry's transformation language gives the word. It has a
// leg per signal, since the log and span dialects answer the same questions
// over different record types, so one table serves both. A getter answers
// with the attribute it read from, the value, and an error only when the
// value was present but unreadable; an empty key means the producer did not
// say. The span leg is declared here and wired when the span transform gets
// the enrichers.
type getter[V columnValue] struct {
	log  func(dialect.LogDialect, *otelv1.InboundLogRecord) (key string, value V, err error)
	span func(dialect.SpanDialect, *otelv1.InboundSpan) (key string, value V, err error)
}

// constantKey is what a constant answers as the attribute it was read from:
// the event type implied the value, so the type is what stated it.
const constantKey = "event.type"

// constant is a getter whose answer the event type implies, such as an
// api_response's outcome being ok.
func constant[V columnValue](value V) getter[V] {
	return getter[V]{
		log: func(dialect.LogDialect, *otelv1.InboundLogRecord) (string, V, error) {
			return constantKey, value, nil
		},
		span: func(dialect.SpanDialect, *otelv1.InboundSpan) (string, V, error) {
			return constantKey, value, nil
		},
	}
}

// condition is a yes-or-no question about a record, with a leg per signal,
// for the table entries whose requirement depends on the record itself.
type condition struct {
	log  func(dialect.LogDialect, *otelv1.InboundLogRecord) bool
	span func(dialect.SpanDialect, *otelv1.InboundSpan) bool
}

// statedBy is the condition that a getter answers with a non-empty key: the
// record stated that value. It expresses a pair such as the MCP server and
// tool, where one half is required once the other half is present.
func statedBy[V columnValue](g getter[V]) condition {
	return condition{
		log: func(d dialect.LogDialect, r *otelv1.InboundLogRecord) bool {
			key, _, err := g.log(d, r)
			return err == nil && key != ""
		},
		span: func(d dialect.SpanDialect, s *otelv1.InboundSpan) bool {
			key, _, err := g.span(d, s)
			return err == nil && key != ""
		},
	}
}

// errNotRequired is a getter's answer when the value is absent and its
// requirement level says that is not a gap: the column stays empty and
// nothing is counted, since nothing is missing.
var errNotRequired = errors.New("column is not required on this record")

// recommended marks an entry at the Recommended level: the producer carries
// the value only sometimes, by nature rather than by omission, such as the
// skill a request was made on behalf of. An absent value is not counted,
// since the counter exists to catch a producer renaming an attribute, and a
// column that is empty most of the time by design would drown that signal.
func recommended[V columnValue](g getter[V]) getter[V] {
	return notCountedWhenAbsent(g)
}

// optIn marks an entry at the Opt-In level: the producer sends the value
// only when the person running the agent agreed to it, such as the words of
// a prompt. An absent value is a choice rather than a gap and is not
// counted.
func optIn[V columnValue](g getter[V]) getter[V] {
	return notCountedWhenAbsent(g)
}

// notCountedWhenAbsent is the one mechanism behind Recommended and Opt-In:
// the two levels differ in why a value may be absent, not in what the
// enricher does about it.
func notCountedWhenAbsent[V columnValue](g getter[V]) getter[V] {
	return getter[V]{
		log: func(d dialect.LogDialect, r *otelv1.InboundLogRecord) (string, V, error) {
			key, value, err := g.log(d, r)
			if err == nil && key == "" {
				return "", value, errNotRequired
			}
			return key, value, err
		},
		span: func(d dialect.SpanDialect, s *otelv1.InboundSpan) (string, V, error) {
			key, value, err := g.span(d, s)
			if err == nil && key == "" {
				return "", value, errNotRequired
			}
			return key, value, err
		},
	}
}

// conditionallyRequired marks an entry at the Conditionally Required level:
// the value is required when the condition holds, such as the message of an
// outcome that is an error. A stated value is written whichever way the
// condition goes; an absent one is a gap only when the condition holds.
func conditionallyRequired[V columnValue](g getter[V], when condition) getter[V] {
	return getter[V]{
		log: func(d dialect.LogDialect, r *otelv1.InboundLogRecord) (string, V, error) {
			key, value, err := g.log(d, r)
			if err == nil && key == "" && !when.log(d, r) {
				return "", value, errNotRequired
			}
			return key, value, err
		},
		span: func(d dialect.SpanDialect, s *otelv1.InboundSpan) (string, V, error) {
			key, value, err := g.span(d, s)
			if err == nil && key == "" && !when.span(d, s) {
				return "", value, errNotRequired
			}
			return key, value, err
		},
	}
}

// perEventType says, per event type, how a column's value is read and at
// which requirement level. Keys are the agent vocabulary's event types; a
// table never names the unclassified type, since an unclassified record gets
// no column enricher.
type perEventType[V columnValue] map[string]getter[V]

// classifiedEventTypes is every event type in the agent vocabulary, for the
// columns that every classified record carries.
var classifiedEventTypes = []string{
	dialect.EventTypePrompt,
	dialect.EventTypeAPIRequest,
	dialect.EventTypeAPIResponse,
	dialect.EventTypeAPIError,
	dialect.EventTypeAPIRefusal,
	dialect.EventTypeToolCall,
	dialect.EventTypeToolCallResult,
	dialect.EventTypeToolDecision,
	dialect.EventTypeAPIRequestBody,
	dialect.EventTypeAPIResponseBody,
	dialect.EventTypeCompaction,
}

// everyClassifiedType builds a table that reads the same getter for every
// classified event type, for the columns such as session_id that any kind
// of event carries.
func everyClassifiedType[V columnValue](g getter[V]) perEventType[V] {
	table := make(perEventType[V], len(classifiedEventTypes))
	for _, eventType := range classifiedEventTypes {
		table[eventType] = g
	}
	return table
}

// logColumnEnricher fills one agent_events column for log records.
type logColumnEnricher[V columnValue] struct {
	column      attribute.Key
	byType      perEventType[V]
	instruments *Instruments
}

func (e *logColumnEnricher[V]) Name() string {
	return "enrich-column-" + columnOf(e.column)
}

func (e *logColumnEnricher[V]) Enrich(ctx context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	d := dialect.ForLog(record)
	eventType := stated(d.EventType(record))
	get, applies := e.byType[eventType]
	if !applies {
		return nil, nil
	}

	key, value, err := get.log(d, record)
	if errors.Is(err, errNotRequired) {
		return nil, nil
	}
	if err != nil || key == "" {
		// The provider did not say, or said something unreadable. Either way
		// the column stays empty: absent, never a guess, and counted so a
		// producer renaming an attribute is visible the same day.
		e.instruments.recordColumnValueMissing(ctx, missingLabelLog(d, record), eventType, columnOf(e.column))
		return nil, nil
	}

	kv, err := columnKeyValue(e.column, value)
	if err != nil {
		return nil, err
	}
	return []attribute.KeyValue{kv}, nil
}

// missingLabelOther is the missing-value counter's surface label for a
// producer whose surface the dialects do not know from its scope, or that
// is not in the agent surface vocabulary.
const missingLabelOther = string(agentsurface.SurfaceOther)

// missingLabelLog is the surface label of the missing-value counter for a
// log record.
func missingLabelLog(d dialect.LogDialect, record *otelv1.InboundLogRecord) string {
	return missingLabel(d.Surface(record))
}

// missingLabel is the surface label the missing-value counter uses: the
// surface the dialect reported, folded into the agent surface vocabulary
// (claude_code, claude_chat, cowork, codex, cursor), or "other" when the
// dialect reported nothing, could not read it, or named a surface outside
// the vocabulary. Folding is what bounds the label set, which is what
// matters for a metric: a producer's free-form service.name is never a
// label. The hooks dialect reports its surface from the attribute Gram's
// own tee stamps rather than from the scope, and folding keeps that
// traffic attributable per surface too.
func missingLabel(_, surface string, err error) string {
	if err != nil || surface == "" {
		return missingLabelOther
	}
	folded, ok := agentsurface.ForHookSource(surface, "")
	if !ok || folded == agentsurface.SurfaceUnknown {
		return missingLabelOther
	}
	return string(folded)
}

// columnOf is the agent_events column a canonical key carries.
func columnOf(key attribute.Key) string {
	return strings.TrimPrefix(string(key), agentColumnKeyPrefix)
}

// columnKeyValue encodes a column's value under its canonical key. A stated
// zero is still stated: a request that read nothing from the cache says so.
func columnKeyValue[V columnValue](key attribute.Key, value V) (attribute.KeyValue, error) {
	switch v := any(value).(type) {
	case string:
		return key.String(v), nil
	case int64:
		return key.Int64(v), nil
	case float64:
		return key.Float64(v), nil
	default:
		return attribute.KeyValue{Key: "", Value: attribute.Value{}}, fmt.Errorf("column %s: unsupported value type %T", columnOf(key), value)
	}
}

// inboundLogSource is the canonical source of an inbound log record, derived
// from the resource's service.name the way the event feed derives it, so the
// source column, the missing-value counter and the feed agree on what to
// call a producer.
func inboundLogSource(record *otelv1.InboundLogRecord) string {
	return CanonicalSource(inboundLogResourceString(record, ServiceNameAttribute))
}

// inboundLogResourceString reads one string attribute off an inbound log
// record's resource, or "" when the resource does not state it.
func inboundLogResourceString(record *otelv1.InboundLogRecord, key string) string {
	for _, kv := range record.GetResource().GetAttributes() {
		if kv.GetKey() == key && kv.GetValue().HasStringValue() {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

// inboundLogAttributeString reads one string attribute off an inbound log
// record, or "" when the record does not carry it as a non-empty string.
func inboundLogAttributeString(record *otelv1.InboundLogRecord, key string) string {
	for _, kv := range record.GetAttributes() {
		if kv.GetKey() == key && kv.GetValue().HasStringValue() {
			return kv.GetValue().GetStringValue()
		}
	}
	return ""
}

// stated keeps a dialect's answer only when it stated one: an empty key or
// a read error means absent, never a guess.
func stated[T any](key string, value T, err error) T {
	var zero T
	if err != nil || key == "" {
		return zero
	}
	return value
}
