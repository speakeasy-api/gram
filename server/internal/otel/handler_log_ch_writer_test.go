package otel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/otel/chrepo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type captureLogInserter struct {
	batches [][]chrepo.OTelLogRow
	err     error
}

func (c *captureLogInserter) InsertOTelLogs(_ context.Context, rows []chrepo.OTelLogRow) error {
	c.batches = append(c.batches, rows)
	return c.err
}

func newLogEventTestWriter(t *testing.T, inserter OTelLogInserter) *LogEventCHWriter {
	t.Helper()
	return NewLogEventCHWriter(testenv.NewLogger(t), testenv.NewMeterProvider(t), inserter)
}

func logEventTestKV(key, value string) *otelv1.LogRecord_KeyValue {
	return (&otelv1.LogRecord_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.LogRecord_AnyValue_builder{StringValue: &value}).Build(),
	}).Build()
}

func logEventTestRecord(recordID, organizationID, serviceName string) *otelv1.LogRecord {
	var resourceAttributes []*otelv1.LogRecord_KeyValue
	if serviceName != "" {
		resourceAttributes = append(resourceAttributes, logEventTestKV("service.name", serviceName))
	}
	return (&otelv1.LogRecord_builder{
		RecordId:             &recordID,
		TimeUnixNano:         new(uint64(1_724_500_000_000_000_001)),
		ObservedTimeUnixNano: new(uint64(1_724_500_000_000_000_002)),
		SeverityNumber:       otelv1.LogRecord_SEVERITY_NUMBER_INFO.Enum(),
		SeverityText:         new("INFO"),
		EventName:            new("api_request"),
		Flags:                new(uint32(1)),
		TraceId:              bytes.Repeat([]byte{0xab}, 16),
		SpanId:               bytes.Repeat([]byte{0xcd}, 8),
		Body:                 (&otelv1.LogRecord_AnyValue_builder{StringValue: new("hello world")}).Build(),
		Attributes: []*otelv1.LogRecord_KeyValue{
			logEventTestKV("gen_ai.conversation.id", "session-1"),
			(&otelv1.LogRecord_KeyValue_builder{
				Key:   new("input_tokens"),
				Value: (&otelv1.LogRecord_AnyValue_builder{IntValue: new(int64(42))}).Build(),
			}).Build(),
		},
		Resource: (&otelv1.LogRecord_Resource_builder{
			Attributes: resourceAttributes,
		}).Build(),
		ResourceSchemaUrl: new("https://opentelemetry.io/schemas/1.27.0"),
		Scope: (&otelv1.LogRecord_InstrumentationScope_builder{
			Name:       new("com.speakeasy.ai.logging"),
			Version:    new("1.2.3"),
			Attributes: []*otelv1.LogRecord_KeyValue{logEventTestKV("scope.key", "scope-value")},
		}).Build(),
		ScopeSchemaUrl: new("https://opentelemetry.io/schemas/1.28.0"),
		Provenance: (&otelv1.LogRecord_Provenance_builder{
			Source:         new("speakeasy"),
			OrganizationId: &organizationID,
			ProjectId:      new("project-1"),
		}).Build(),
	}).Build()
}

func TestLogEventCHWriterMapsRecordToRow(t *testing.T) {
	t.Parallel()

	inserter := &captureLogInserter{batches: nil, err: nil}
	writer := newLogEventTestWriter(t, inserter)

	record := logEventTestRecord("record-1", "org-1", "claude-code")
	require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.LogRecord{record}, nil))

	require.Len(t, inserter.batches, 1)
	require.Len(t, inserter.batches[0], 1)
	row := inserter.batches[0][0]

	require.Equal(t, "record-1", row.RecordID)
	require.Equal(t, "org-1", row.OrganizationID)
	require.Equal(t, "project-1", row.ProjectID)
	require.Equal(t, int64(1_724_500_000_000_000_001), row.TimeUnixNano)
	require.Equal(t, int64(1_724_500_000_000_000_002), row.ObservedTimeUnixNano)
	require.Equal(t, "claude-code", row.Source)
	require.Equal(t, "abababababababababababababababab", row.TraceID)
	require.Equal(t, "cdcdcdcdcdcdcdcd", row.SpanID)
	require.Equal(t, "api_request", row.EventName)
	require.Equal(t, "INFO", row.SeverityText)
	require.Equal(t, int32(otelv1.LogRecord_SEVERITY_NUMBER_INFO), row.SeverityNumber)
	require.Equal(t, "hello world", row.Body)
	require.Equal(t, uint32(1), row.Flags)
	require.Equal(t, "https://opentelemetry.io/schemas/1.27.0", row.ResourceSchemaURL)
	require.Equal(t, "com.speakeasy.ai.logging", row.ScopeName)
	require.Equal(t, "1.2.3", row.ScopeVersion)

	var logAttributes map[string]any
	require.NoError(t, json.Unmarshal([]byte(row.LogAttributes), &logAttributes))
	require.Equal(t, "session-1", logAttributes["gen_ai.conversation.id"])
	require.InDelta(t, 42, logAttributes["input_tokens"], 0)

	var resourceAttributes map[string]any
	require.NoError(t, json.Unmarshal([]byte(row.ResourceAttributes), &resourceAttributes))
	require.Equal(t, "claude-code", resourceAttributes["service.name"])

	var scopeAttributes map[string]any
	require.NoError(t, json.Unmarshal([]byte(row.ScopeAttributes), &scopeAttributes))
	require.Equal(t, "scope-value", scopeAttributes["scope.key"])
}

func TestLogEventCHWriterCanonicalizesSourceFromServiceName(t *testing.T) {
	t.Parallel()

	inserter := &captureLogInserter{batches: nil, err: nil}
	writer := newLogEventTestWriter(t, inserter)

	records := []*otelv1.LogRecord{
		logEventTestRecord("record-1", "org-1", "Claude Code"),
		logEventTestRecord("record-2", "org-1", "LiteLLM"),
		logEventTestRecord("record-3", "org-1", ""),
	}
	require.NoError(t, writer.HandleBatch(t.Context(), records, nil))

	require.Len(t, inserter.batches, 1)
	rows := inserter.batches[0]
	require.Len(t, rows, 3)
	require.Equal(t, "claude-code", rows[0].Source)
	require.Equal(t, "litellm", rows[1].Source)
	require.Equal(t, "unknown", rows[2].Source)
}

func TestLogEventCHWriterFallsBackToObservedTime(t *testing.T) {
	t.Parallel()

	inserter := &captureLogInserter{batches: nil, err: nil}
	writer := newLogEventTestWriter(t, inserter)

	record := logEventTestRecord("record-1", "org-1", "claude-code")
	record.SetTimeUnixNano(0)
	require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.LogRecord{record}, nil))

	require.Len(t, inserter.batches, 1)
	require.Equal(t, int64(1_724_500_000_000_000_002), inserter.batches[0][0].TimeUnixNano)
}

func TestLogEventCHWriterStampsWriteTimeWhenBothTimesMissing(t *testing.T) {
	t.Parallel()

	inserter := &captureLogInserter{batches: nil, err: nil}
	writer := newLogEventTestWriter(t, inserter)

	start := time.Now().UnixNano()
	record := logEventTestRecord("record-1", "org-1", "claude-code")
	record.SetTimeUnixNano(0)
	record.SetObservedTimeUnixNano(0)
	require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.LogRecord{record}, nil))

	require.Len(t, inserter.batches, 1)
	row := inserter.batches[0][0]
	require.GreaterOrEqual(t, row.TimeUnixNano, start)
	require.Zero(t, row.ObservedTimeUnixNano)
}

func TestLogEventCHWriterEncodesStructuredBody(t *testing.T) {
	t.Parallel()

	inserter := &captureLogInserter{batches: nil, err: nil}
	writer := newLogEventTestWriter(t, inserter)

	record := logEventTestRecord("record-1", "org-1", "claude-code")
	record.SetBody((&otelv1.LogRecord_AnyValue_builder{
		KvlistValue: (&otelv1.LogRecord_KeyValueList_builder{
			Values: []*otelv1.LogRecord_KeyValue{logEventTestKV("message", "structured")},
		}).Build(),
	}).Build())
	require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.LogRecord{record}, nil))

	require.Len(t, inserter.batches, 1)
	require.JSONEq(t, `{"message":"structured"}`, inserter.batches[0][0].Body)
}

func TestLogEventCHWriterSkipsUnprocessableRecords(t *testing.T) {
	t.Parallel()

	inserter := &captureLogInserter{batches: nil, err: nil}
	writer := newLogEventTestWriter(t, inserter)

	missingRecordID := logEventTestRecord("", "org-1", "claude-code")
	missingOrganization := logEventTestRecord("record-2", "", "claude-code")
	valid := logEventTestRecord("record-3", "org-1", "claude-code")

	messages := []*otelv1.LogRecord{nil, missingRecordID, missingOrganization, valid}
	require.NoError(t, writer.HandleBatch(t.Context(), messages, nil))

	require.Len(t, inserter.batches, 1)
	rows := inserter.batches[0]
	require.Len(t, rows, 1)
	require.Equal(t, "record-3", rows[0].RecordID)
}

func TestLogEventCHWriterAcksBatchOfOnlyUnprocessableRecords(t *testing.T) {
	t.Parallel()

	inserter := &captureLogInserter{batches: nil, err: nil}
	writer := newLogEventTestWriter(t, inserter)

	require.NoError(t, writer.HandleBatch(t.Context(), []*otelv1.LogRecord{nil}, nil))
	require.Empty(t, inserter.batches)
}

func TestLogEventCHWriterReturnsInsertErrorForRedelivery(t *testing.T) {
	t.Parallel()

	inserter := &captureLogInserter{batches: nil, err: errors.New("clickhouse unavailable")}
	writer := newLogEventTestWriter(t, inserter)

	record := logEventTestRecord("record-1", "org-1", "claude-code")
	err := writer.HandleBatch(t.Context(), []*otelv1.LogRecord{record}, nil)
	require.ErrorContains(t, err, "clickhouse unavailable")
}
