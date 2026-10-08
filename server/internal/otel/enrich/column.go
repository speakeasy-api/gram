package enrich

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/agentsurface"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"go.opentelemetry.io/otel/attribute"
)

// A column is declared once as a table from event type to getter and serves
// both signals. A type absent from the table never gets the column; a type in
// the table whose producer stated nothing gets no value and is counted when
// the entry is Required. Entries carry OpenTelemetry's attribute requirement
// levels (https://opentelemetry.io/docs/specs/semconv/general/attribute-requirement-level/):
// a bare entry is Required, and recommended, optIn and conditionallyRequired
// say when an absence is not a gap.

type columnValue interface {
	string | int64 | float64
}

// getter reads one column's value for one event type, with a leg per signal.
// An empty key means the producer did not state the value.
type getter[V columnValue] struct {
	log  func(dialect.LogDialect, *otelv1.InboundLogRecord) (key string, value V, err error)
	span func(dialect.SpanDialect, *otelv1.InboundSpan) (key string, value V, err error)
}

// constantKey is the key a constant answers with: the event type stated it.
const constantKey = "event.type"

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

type condition struct {
	log  func(dialect.LogDialect, *otelv1.InboundLogRecord) bool
	span func(dialect.SpanDialect, *otelv1.InboundSpan) bool
}

// statedBy holds when the getter answers with a non-empty key.
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

// errNotRequired is a getter's answer when an absent value is not a gap.
var errNotRequired = errors.New("column is not required on this record")

func recommended[V columnValue](g getter[V]) getter[V] {
	return notCountedWhenAbsent(g)
}

func optIn[V columnValue](g getter[V]) getter[V] {
	return notCountedWhenAbsent(g)
}

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

// conditionallyRequired counts an absence only when the condition holds; a
// stated value is written either way.
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

// perEventType never names the unclassified type: an unclassified record gets
// no column.
type perEventType[V columnValue] map[string]getter[V]

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

func everyClassifiedType[V columnValue](g getter[V]) perEventType[V] {
	table := make(perEventType[V], len(classifiedEventTypes))
	for _, eventType := range classifiedEventTypes {
		table[eventType] = g
	}
	return table
}

// columnDefinition is one column declared once, yielding an enricher per
// signal.
type columnDefinition interface {
	name() string
	log(in *Instruments) LogEnricher
	span(in *Instruments) SpanEnricher
}

type column[V columnValue] struct {
	key    attribute.Key
	byType perEventType[V]
}

func (c column[V]) name() string { return columnOf(c.key) }

func (c column[V]) log(in *Instruments) LogEnricher {
	return &logColumnEnricher[V]{column: c, instruments: in, capBytes: 0}
}

func (c column[V]) span(in *Instruments) SpanEnricher {
	return &spanColumnEnricher[V]{column: c, instruments: in, capBytes: 0}
}

// cappedColumn is a string column whose canonical copy is cut at capBytes on a
// character boundary and counted, so a value with no natural size cannot push
// a near-limit record past what a relay export may carry.
type cappedColumn struct {
	column[string]
	capBytes int
}

func (c cappedColumn) log(in *Instruments) LogEnricher {
	return &logColumnEnricher[string]{column: c.column, instruments: in, capBytes: c.capBytes}
}

func (c cappedColumn) span(in *Instruments) SpanEnricher {
	return &spanColumnEnricher[string]{column: c.column, instruments: in, capBytes: c.capBytes}
}

type logColumnEnricher[V columnValue] struct {
	column      column[V]
	instruments *Instruments
	capBytes    int
}

func (e *logColumnEnricher[V]) Name() string {
	return "enrich-column-" + e.column.name()
}

func (e *logColumnEnricher[V]) Enrich(ctx context.Context, record *otelv1.InboundLogRecord) ([]attribute.KeyValue, error) {
	d := dialect.ForLog(record)
	return insertColumn(ctx, e.instruments, e.column, e.capBytes, stated(d.EventType(record)), func() string {
		return missingLabelLog(d, record)
	}, func(get getter[V]) (string, V, error) {
		return get.log(d, record)
	})
}

type spanColumnEnricher[V columnValue] struct {
	column      column[V]
	instruments *Instruments
	capBytes    int
}

func (e *spanColumnEnricher[V]) Name() string {
	return "enrich-column-" + e.column.name()
}

func (e *spanColumnEnricher[V]) Enrich(ctx context.Context, span *otelv1.InboundSpan) ([]attribute.KeyValue, error) {
	d := dialect.ForSpan(span)
	return insertColumn(ctx, e.instruments, e.column, e.capBytes, stated(d.EventType(span)), func() string {
		return missingLabelSpan(d, span)
	}, func(get getter[V]) (string, V, error) {
		return get.span(d, span)
	})
}

// insertColumn looks the event type up in the column's table, reads the
// getter and writes the value, or counts its absence. It never overwrites.
// The surface label is read lazily, since only a count needs it.
func insertColumn[V columnValue](
	ctx context.Context,
	in *Instruments,
	c column[V],
	capBytes int,
	eventType string,
	surface func() string,
	read func(getter[V]) (string, V, error),
) ([]attribute.KeyValue, error) {
	get, applies := c.byType[eventType]
	if !applies {
		return nil, nil
	}

	key, value, err := read(get)
	if errors.Is(err, errNotRequired) {
		return nil, nil
	}
	if err != nil || key == "" {
		// Absent or unreadable: the column stays empty, never a guess.
		in.recordColumnValueMissing(ctx, surface(), eventType, c.name())
		return nil, nil
	}

	if text, ok := any(value).(string); ok && capBytes > 0 && len(text) > capBytes {
		in.recordColumnValueTruncated(ctx, surface(), eventType, c.name())
		return []attribute.KeyValue{c.key.String(truncateUTF8(text, capBytes))}, nil
	}

	kv, err := columnKeyValue(c.key, value)
	if err != nil {
		return nil, err
	}
	return []attribute.KeyValue{kv}, nil
}

func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

const missingLabelOther = string(agentsurface.SurfaceOther)

func missingLabelLog(d dialect.LogDialect, record *otelv1.InboundLogRecord) string {
	return missingLabel(d.Surface(record))
}

func missingLabelSpan(d dialect.SpanDialect, span *otelv1.InboundSpan) string {
	return missingLabel(d.Surface(span))
}

// missingLabel folds the surface the dialect reported into the agent surface
// vocabulary, or "other". Folding is what keeps the counter's label set
// bounded: a producer's free-form service.name is never a label.
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

func columnOf(key attribute.Key) string {
	return strings.TrimPrefix(string(key), agentColumnKeyPrefix)
}

// columnKeyValue writes a stated zero as a value, not an absence.
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

// inboundLogSource derives the source from the resource's service.name the
// way the event feed does, so every consumer calls a producer the same thing.
func inboundLogSource(record *otelv1.InboundLogRecord) string {
	return CanonicalSource(inboundLogResourceString(record, ServiceNameAttribute))
}

func inboundSpanSource(span *otelv1.InboundSpan) string {
	return CanonicalSource(inboundSpanResourceString(span, ServiceNameAttribute))
}

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

// stated keeps a dialect's answer only when it stated one.
func stated[T any](key string, value T, err error) T {
	var zero T
	if err != nil || key == "" {
		return zero
	}
	return value
}
