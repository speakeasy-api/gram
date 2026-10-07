package enrich

import (
	"context"
	"fmt"
	"strings"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// A column enricher fills one agent_events column in the transform stage.
// It holds a table from event type to a getter: how the value is read for
// that type, a question the provider dialect answers or a constant the type
// implies. The value lands on the record as a canonical
// speakeasy.event.<column> attribute, next to the producer's original
// attributes, and the agent_events writer copies it from there. The file
// that declares a column's table is the documentation of that column.
//
// The rules every table follows:
//
//   - An event type absent from the table means the column is never set for
//     that type. There is no forbidden list: a tool result never gets tokens
//     because the tokens table does not name tool results.
//   - A type in the table whose provider stated nothing writes nothing and is
//     counted by source, event type and column. Absent is never an error, and
//     never a guess.
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

// perEventType says, per event type, how a column's value is read. Keys are
// the agent vocabulary's event types; a table never names the unclassified
// type, since an unclassified record gets no column enricher.
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
	if err != nil || key == "" {
		// The provider did not say, or said something unreadable. Either way
		// the column stays empty: absent, never a guess, and counted so a
		// producer renaming an attribute is visible the same day.
		e.instruments.recordColumnValueMissing(ctx, inboundLogSource(record), eventType, columnOf(e.column))
		return nil, nil
	}

	kv, err := columnKeyValue(e.column, value)
	if err != nil {
		return nil, err
	}
	return []attribute.KeyValue{kv}, nil
}

// columnName is the agent_events column a canonical key carries.
func columnOf(key attribute.Key) string {
	return strings.TrimPrefix(string(key), eventColumnKeyPrefix)
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
