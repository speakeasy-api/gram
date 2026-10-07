package enrich

import (
	"context"
	"errors"
	"fmt"
	"strings"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// A column enricher fills one agent_events column in the transform stage.
// It holds a table from event type to how the value is read for that type:
// a question the provider dialect answers, or a constant the type implies.
// The value lands on the record as a canonical speakeasy.agent.<column>
// attribute, next to the producer's original attributes, and the
// agent_events writer copies it from there. The file that declares a
// column's table is the documentation of that column.
//
// A column is declared once and serves both signals: every question has a
// log leg and a span leg, and a column definition yields a log enricher and
// a span enricher from the same table, so the two cannot drift.
//
// The rules every table follows:
//
//   - An event type absent from the table means the column is never set for
//     that type. There is no forbidden list: a tool result never gets tokens
//     because the tokens table does not name tool results.
//   - A type in the table whose provider stated nothing writes nothing and is
//     counted by source, event type and column. Absent is never an error, and
//     never a guess.
//   - A question may answer that the record has no such value by its own
//     content (a built-in tool has no MCP server). Nothing is written and
//     nothing is counted, since nothing is missing.
//   - An unclassified record matches no table, so no column enricher applies
//     to it. It still lands with its payload.

// columnValue is what a column enricher can write: the agent_events columns
// the enrichers fill are strings, integers and floating-point numbers.
type columnValue interface {
	string | int64 | float64
}

// question is how one column's value is read for one event type. It has a
// leg per signal, since the log and span dialects answer the same questions
// over different record types, so one table serves both.
type question[V columnValue] struct {
	log  func(dialect.LogDialect, *otelv1.InboundLogRecord) (key string, value V, err error)
	span func(dialect.SpanDialect, *otelv1.InboundSpan) (key string, value V, err error)
}

// constantKey is what a constant answers as the attribute it was read from:
// the event type implied the value, so the type is what stated it.
const constantKey = "event.type"

// constant is a question whose answer the event type implies, such as an
// api_response's outcome being ok.
func constant[V columnValue](value V) question[V] {
	return question[V]{
		log: func(dialect.LogDialect, *otelv1.InboundLogRecord) (string, V, error) {
			return constantKey, value, nil
		},
		span: func(dialect.SpanDialect, *otelv1.InboundSpan) (string, V, error) {
			return constantKey, value, nil
		},
	}
}

// errNotApplicable is a question's answer when the record, by its own
// content, has no such value: a built-in tool has no MCP server, a result
// that succeeded has no error message. The column stays empty and nothing is
// counted, since nothing is missing.
var errNotApplicable = errors.New("column does not apply to this record")

// optional marks a question whose answer a type carries only sometimes, by
// nature rather than by a producer's omission: a request made on behalf of a
// skill names the skill and every other request names none. An absent answer
// is then not counted as missing, since the counter exists to catch a
// producer renaming an attribute, and a column that is empty most of the
// time by design would drown that signal.
func optional[V columnValue](q question[V]) question[V] {
	return question[V]{
		log: func(d dialect.LogDialect, r *otelv1.InboundLogRecord) (string, V, error) {
			key, value, err := q.log(d, r)
			if err == nil && key == "" {
				return "", value, errNotApplicable
			}
			return key, value, err
		},
		span: func(d dialect.SpanDialect, s *otelv1.InboundSpan) (string, V, error) {
			key, value, err := q.span(d, s)
			if err == nil && key == "" {
				return "", value, errNotApplicable
			}
			return key, value, err
		},
	}
}

// columnTable says, per event type, how a column's value is read. Keys are
// the agent vocabulary's event types; a table never names the unclassified
// type, since an unclassified record gets no column enricher.
type columnTable[V columnValue] map[string]question[V]

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

// everyClassifiedType builds a table that asks the same question of every
// classified event type, for the columns such as session_id that any kind
// of event carries.
func everyClassifiedType[V columnValue](q question[V]) columnTable[V] {
	table := make(columnTable[V], len(classifiedEventTypes))
	for _, eventType := range classifiedEventTypes {
		table[eventType] = q
	}
	return table
}

// columnDefinition is one agent_events column as declared in its file: the
// key it is written under and, per event type, how it is read. It yields
// the enricher for each signal, so a column declared once serves both.
type columnDefinition interface {
	name() string
	log(in *Instruments) LogEnricher
	span(in *Instruments) SpanEnricher
}

// column is the one columnDefinition, generic over the value it writes.
type column[V columnValue] struct {
	key    attribute.Key
	byType columnTable[V]
}

func (c column[V]) name() string { return columnOf(c.key) }

func (c column[V]) log(in *Instruments) LogEnricher {
	return &logColumnEnricher[V]{column: c, instruments: in}
}

func (c column[V]) span(in *Instruments) SpanEnricher {
	return &spanColumnEnricher[V]{column: c, instruments: in}
}

// logColumnEnricher fills one agent_events column for log records.
type logColumnEnricher[V columnValue] struct {
	column      column[V]
	instruments *Instruments
}

func (e *logColumnEnricher[V]) Name() string {
	return "enrich-column-" + e.column.name()
}

func (e *logColumnEnricher[V]) Enrich(ctx context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	d := dialect.ForLog(record)
	return answerColumn(ctx, e.instruments, e.column, stated(d.EventType(record)), func() string {
		return counterSurfaceLog(d, record)
	}, func(ask question[V]) (string, V, error) {
		return ask.log(d, record)
	})
}

// spanColumnEnricher fills one agent_events column for spans, from the same
// table as the log enricher for that column.
type spanColumnEnricher[V columnValue] struct {
	column      column[V]
	instruments *Instruments
}

func (e *spanColumnEnricher[V]) Name() string {
	return "enrich-column-" + e.column.name()
}

func (e *spanColumnEnricher[V]) Enrich(ctx context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
	d := dialect.ForSpan(span)
	return answerColumn(ctx, e.instruments, e.column, stated(d.EventType(span)), func() string {
		return counterSurfaceSpan(d, span)
	}, func(ask question[V]) (string, V, error) {
		return ask.span(d, span)
	})
}

// answerColumn is the one decision behind both signals: look the event type
// up in the column's table, ask the question the table names, and write the
// answer or count its absence. The surface label is read lazily, since it
// is only needed to count a missing value.
func answerColumn[V columnValue](
	ctx context.Context,
	in *Instruments,
	c column[V],
	eventType string,
	surface func() string,
	answer func(question[V]) (string, V, error),
) ([]attribute.KeyValue, error) {
	ask, applies := c.byType[eventType]
	if !applies {
		return nil, nil
	}

	key, value, err := answer(ask)
	if errors.Is(err, errNotApplicable) {
		return nil, nil
	}
	if err != nil || key == "" {
		// The provider did not say, or said something unreadable. Either way
		// the column stays empty: absent, never a guess, and counted so a
		// producer renaming an attribute is visible the same day.
		in.recordColumnValueMissing(ctx, surface(), eventType, c.name())
		return nil, nil
	}

	kv, err := columnKeyValue(c.key, value)
	if err != nil {
		return nil, err
	}
	return []attribute.KeyValue{kv}, nil
}

// counterSurfaceOther is the missing-value counter's surface label for a
// producer the dialects do not recognise.
const counterSurfaceOther = "other"

// counterSurfaceLog is the surface label of the missing-value counter for a
// log record: the agent surface the dialect recognised, which is a small
// fixed set, or "other". The producer's service.name is not used as a label
// because it is free-form, and a label a producer controls would make the
// counter's series unbounded.
func counterSurfaceLog(d dialect.LogDialect, record *otelv1.InboundLogRecord) string {
	if surface := stated(d.Surface(record)); surface != "" {
		return surface
	}
	return counterSurfaceOther
}

// counterSurfaceSpan is counterSurfaceLog for a span.
func counterSurfaceSpan(d dialect.SpanDialect, span *otelv1.InboundSpan) string {
	if surface := stated(d.Surface(span)); surface != "" {
		return surface
	}
	return counterSurfaceOther
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

// inboundSpanSource is inboundLogSource for a span.
func inboundSpanSource(span *otelv1.InboundSpan) string {
	return CanonicalSource(inboundSpanResourceString(span, ServiceNameAttribute))
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

func inboundSpanResourceString(span *otelv1.InboundSpan, key string) string {
	for _, kv := range span.GetResource().GetAttributes() {
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

func inboundSpanAttributeString(span *otelv1.InboundSpan, key string) string {
	for _, kv := range span.GetAttributes() {
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
