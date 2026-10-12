package otel

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateSpanAcceptsCompleteSpan(t *testing.T) {
	t.Parallel()

	traceID := make([]byte, 16)
	traceID[0] = 1
	spanID := make([]byte, 8)
	spanID[0] = 2
	err := validateSpan(stubSpanLike{
		traceID: traceID,
		spanID:  spanID,
		name:    "operation",
		start:   10,
		end:     20,
	})

	require.NoError(t, err)
}

func TestValidateSpanRejectsInvalidFields(t *testing.T) {
	t.Parallel()

	traceID := make([]byte, 16)
	traceID[0] = 1
	spanID := make([]byte, 8)
	spanID[0] = 2
	require.ErrorContains(t, validateSpan(nil), "span is nil")
	require.ErrorContains(t, validateSpan(stubSpanLike{name: "operation", spanID: spanID, start: 10, end: 20}), "trace_id is empty")
	require.ErrorContains(t, validateSpan(stubSpanLike{name: "operation", traceID: traceID, start: 10, end: 20}), "span_id is empty")
	require.ErrorContains(t, validateSpan(stubSpanLike{name: "operation", traceID: make([]byte, 15), spanID: spanID, start: 10, end: 20}), "trace_id must be 16 bytes")
	require.ErrorContains(t, validateSpan(stubSpanLike{name: "operation", traceID: traceID, spanID: make([]byte, 7), start: 10, end: 20}), "span_id must be 8 bytes")
	require.ErrorContains(t, validateSpan(stubSpanLike{traceID: traceID, spanID: spanID, name: "  ", start: 10, end: 20}), "name is empty")
	require.ErrorContains(t, validateSpan(stubSpanLike{traceID: traceID, spanID: spanID, name: "operation", end: 20}), "start_time_unix_nano is zero")
	require.ErrorContains(t, validateSpan(stubSpanLike{traceID: traceID, spanID: spanID, name: "operation", start: 10}), "end_time_unix_nano is zero")
	require.ErrorContains(t, validateSpan(stubSpanLike{traceID: traceID, spanID: spanID, name: "operation", start: 20, end: 10}), "before start_time_unix_nano")
}

type stubSpanLike struct {
	traceID []byte
	spanID  []byte
	name    string
	start   uint64
	end     uint64
}

func (s stubSpanLike) GetTraceId() []byte           { return s.traceID }
func (s stubSpanLike) GetSpanId() []byte            { return s.spanID }
func (s stubSpanLike) GetName() string              { return s.name }
func (s stubSpanLike) GetStartTimeUnixNano() uint64 { return s.start }
func (s stubSpanLike) GetEndTimeUnixNano() uint64   { return s.end }
