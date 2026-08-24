package otel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/chrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type captureTraceInserter struct {
	batches [][]chrepo.OTelTraceRow
	err     error
}

func (c *captureTraceInserter) InsertOTelTraces(_ context.Context, rows []chrepo.OTelTraceRow) error {
	c.batches = append(c.batches, rows)
	return c.err
}

func newSpanEventTestWriter(t *testing.T, inserter OTelTraceInserter) *SpanEventCHWriter {
	t.Helper()
	return NewSpanEventCHWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), inserter)
}

func spanEventTestKV(key, value string) *otelv1.Span_KeyValue {
	return (&otelv1.Span_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.Span_AnyValue_builder{StringValue: &value}).Build(),
	}).Build()
}

func spanEventTestSpan(organizationID, serviceName string) *otelv1.Span {
	var resourceAttributes []*otelv1.Span_KeyValue
	if serviceName != "" {
		resourceAttributes = append(resourceAttributes, spanEventTestKV("service.name", serviceName))
	}
	return (&otelv1.Span_builder{
		TraceId:           bytes.Repeat([]byte{0xab}, 16),
		SpanId:            bytes.Repeat([]byte{0xcd}, 8),
		ParentSpanId:      bytes.Repeat([]byte{0xef}, 8),
		TraceState:        new("vendor=state"),
		Name:              new("claude_code.api_request"),
		Kind:              otelv1.Span_SPAN_KIND_CLIENT.Enum(),
		StartTimeUnixNano: new(uint64(1_724_500_000_000_000_001)),
		EndTimeUnixNano:   new(uint64(1_724_500_000_000_000_501)),
		Attributes: []*otelv1.Span_KeyValue{
			spanEventTestKV("session.id", "session-1"),
		},
		Status: (&otelv1.Span_Status_builder{
			Message: new("boom"),
			Code:    otelv1.Span_STATUS_CODE_ERROR.Enum(),
		}).Build(),
		Resource: (&otelv1.Span_Resource_builder{
			Attributes: resourceAttributes,
		}).Build(),
		ResourceSchemaUrl: new("https://opentelemetry.io/schemas/1.27.0"),
		Scope: (&otelv1.Span_InstrumentationScope_builder{
			Name:       new("com.speakeasy.ai.logging"),
			Version:    new("1.2.3"),
			Attributes: []*otelv1.Span_KeyValue{spanEventTestKV("scope.key", "scope-value")},
		}).Build(),
		ScopeSchemaUrl: new("https://opentelemetry.io/schemas/1.28.0"),
		Provenance: (&otelv1.Span_Provenance_builder{
			Source:         new("speakeasy"),
			OrganizationId: &organizationID,
			ProjectId:      new("project-1"),
		}).Build(),
	}).Build()
}

func TestSpanEventCHWriterMapsSpanToRow(t *testing.T) {
	t.Parallel()

	inserter := &captureTraceInserter{batches: nil, err: nil}
	writer := newSpanEventTestWriter(t, inserter)

	span := spanEventTestSpan("org-1", "claude-code")
	require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.Span{span}, nil))

	require.Len(t, inserter.batches, 1)
	require.Len(t, inserter.batches[0], 1)
	row := inserter.batches[0][0]

	require.Equal(t, "abababababababababababababababab:cdcdcdcdcdcdcdcd", row.RecordID)
	require.Equal(t, "org-1", row.OrganizationID)
	require.Equal(t, "project-1", row.ProjectID)
	require.Equal(t, int64(1_724_500_000_000_000_001), row.TimeUnixNano)
	require.Equal(t, int64(500), row.DurationNano)
	require.Equal(t, "claude-code", row.Source)
	require.Equal(t, "abababababababababababababababab", row.TraceID)
	require.Equal(t, "cdcdcdcdcdcdcdcd", row.SpanID)
	require.Equal(t, "efefefefefefefef", row.ParentSpanID)
	require.Equal(t, "claude_code.api_request", row.SpanName)
	require.Equal(t, "client", row.SpanKind)
	require.Equal(t, "error", row.StatusCode)
	require.Equal(t, "boom", row.StatusMessage)
	require.Equal(t, "vendor=state", row.TraceState)
	require.Equal(t, "https://opentelemetry.io/schemas/1.27.0", row.ResourceSchemaURL)
	require.Equal(t, "com.speakeasy.ai.logging", row.ScopeName)
	require.Equal(t, "1.2.3", row.ScopeVersion)

	var spanAttributes map[string]any
	require.NoError(t, json.Unmarshal([]byte(row.SpanAttributes), &spanAttributes))
	require.Equal(t, "session-1", spanAttributes["session.id"])

	var resourceAttributes map[string]any
	require.NoError(t, json.Unmarshal([]byte(row.ResourceAttributes), &resourceAttributes))
	require.Equal(t, "claude-code", resourceAttributes["service.name"])

	var scopeAttributes map[string]any
	require.NoError(t, json.Unmarshal([]byte(row.ScopeAttributes), &scopeAttributes))
	require.Equal(t, "scope-value", scopeAttributes["scope.key"])
}

func TestSpanEventCHWriterMapsRootSpanAndDefaultEnums(t *testing.T) {
	t.Parallel()

	inserter := &captureTraceInserter{batches: nil, err: nil}
	writer := newSpanEventTestWriter(t, inserter)

	span := spanEventTestSpan("org-1", "claude-code")
	span.SetParentSpanId(nil)
	span.SetKind(otelv1.Span_SPAN_KIND_UNSPECIFIED)
	span.ClearStatus()
	require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.Span{span}, nil))

	require.Len(t, inserter.batches, 1)
	row := inserter.batches[0][0]
	require.Empty(t, row.ParentSpanID)
	require.Equal(t, "unspecified", row.SpanKind)
	require.Equal(t, "unspecified", row.StatusCode)
	require.Empty(t, row.StatusMessage)
}

func TestSpanEventCHWriterCanonicalizesSourceFromServiceName(t *testing.T) {
	t.Parallel()

	inserter := &captureTraceInserter{batches: nil, err: nil}
	writer := newSpanEventTestWriter(t, inserter)

	withSlugCase := spanEventTestSpan("org-1", "Claude Code")
	withoutServiceName := spanEventTestSpan("org-1", "")
	require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.Span{withSlugCase, withoutServiceName}, nil))

	require.Len(t, inserter.batches, 1)
	rows := inserter.batches[0]
	require.Len(t, rows, 2)
	require.Equal(t, "claude-code", rows[0].Source)
	require.Equal(t, "unknown", rows[1].Source)
}

func TestSpanEventCHWriterFallsBackToEndTimeForMissingStart(t *testing.T) {
	t.Parallel()

	inserter := &captureTraceInserter{batches: nil, err: nil}
	writer := newSpanEventTestWriter(t, inserter)

	span := spanEventTestSpan("org-1", "claude-code")
	span.SetStartTimeUnixNano(0)
	require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.Span{span}, nil))

	require.Len(t, inserter.batches, 1)
	row := inserter.batches[0][0]
	require.Equal(t, int64(1_724_500_000_000_000_501), row.TimeUnixNano)
	require.Zero(t, row.DurationNano)
}

func TestSpanEventCHWriterSkipsSpanWhenBothTimesMissing(t *testing.T) {
	t.Parallel()

	inserter := &captureTraceInserter{batches: nil, err: nil}
	writer := newSpanEventTestWriter(t, inserter)

	// A wall-clock fallback would give redeliveries of the same span
	// different sort keys, defeating ReplacingMergeTree dedup, so spans
	// without any timestamp are unprocessable.
	span := spanEventTestSpan("org-1", "claude-code")
	span.SetStartTimeUnixNano(0)
	span.SetEndTimeUnixNano(0)
	require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.Span{span}, nil))

	require.Empty(t, inserter.batches)
}

func TestSpanEventCHWriterCapsAttributeNestingDepth(t *testing.T) {
	t.Parallel()

	inserter := &captureTraceInserter{batches: nil, err: nil}
	writer := newSpanEventTestWriter(t, inserter)

	nested := (&otelv1.Span_AnyValue_builder{StringValue: new("leaf")}).Build()
	for range 2 * maxEventAnyValueDepth {
		nested = (&otelv1.Span_AnyValue_builder{
			ArrayValue: (&otelv1.Span_ArrayValue_builder{
				Values: []*otelv1.Span_AnyValue{nested},
			}).Build(),
		}).Build()
	}
	span := spanEventTestSpan("org-1", "claude-code")
	span.SetAttributes([]*otelv1.Span_KeyValue{
		(&otelv1.Span_KeyValue_builder{Key: new("deep"), Value: nested}).Build(),
	})

	require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.Span{span}, nil))

	require.Len(t, inserter.batches, 1)
	var spanAttributes map[string]any
	require.NoError(t, json.Unmarshal([]byte(inserter.batches[0][0].SpanAttributes), &spanAttributes))
	require.Contains(t, spanAttributes, "deep")
}

func TestSpanEventCHWriterSkipsUnprocessableSpans(t *testing.T) {
	t.Parallel()

	inserter := &captureTraceInserter{batches: nil, err: nil}
	writer := newSpanEventTestWriter(t, inserter)

	missingOrganization := spanEventTestSpan("", "claude-code")
	zeroTraceID := spanEventTestSpan("org-1", "claude-code")
	zeroTraceID.SetTraceId(make([]byte, 16))
	valid := spanEventTestSpan("org-1", "claude-code")

	messages := []*otelv1.Span{nil, missingOrganization, zeroTraceID, valid}
	require.NoError(t, writer.HandleBatch(t.Context(), messages, nil))

	require.Len(t, inserter.batches, 1)
	rows := inserter.batches[0]
	require.Len(t, rows, 1)
	require.Equal(t, "org-1", rows[0].OrganizationID)
}

func TestSpanEventCHWriterReturnsInsertErrorForRedelivery(t *testing.T) {
	t.Parallel()

	inserter := &captureTraceInserter{batches: nil, err: errors.New("clickhouse unavailable")}
	writer := newSpanEventTestWriter(t, inserter)

	span := spanEventTestSpan("org-1", "claude-code")
	err := writer.HandleBatch(t.Context(), []*otelv1.Span{span}, nil)
	require.ErrorContains(t, err, "clickhouse unavailable")
}
