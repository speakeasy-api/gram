package otel

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	otlpTraceIDSize = 16
	otlpSpanIDSize  = 8
)

type spanLike interface {
	GetTraceId() []byte
	GetSpanId() []byte
	GetName() string
	GetStartTimeUnixNano() uint64
	GetEndTimeUnixNano() uint64
}

func validateSpan(span spanLike) error {
	if span == nil {
		return oops.E(oops.CodeBadRequest, nil, "span is nil")
	}
	if len(span.GetTraceId()) == 0 {
		return oops.E(oops.CodeBadRequest, nil, "span trace_id is empty")
	}
	if len(span.GetTraceId()) != otlpTraceIDSize {
		return oops.E(oops.CodeBadRequest, nil, "span trace_id must be %d bytes", otlpTraceIDSize)
	}
	if len(span.GetSpanId()) == 0 {
		return oops.E(oops.CodeBadRequest, nil, "span span_id is empty")
	}
	if len(span.GetSpanId()) != otlpSpanIDSize {
		return oops.E(oops.CodeBadRequest, nil, "span span_id must be %d bytes", otlpSpanIDSize)
	}
	if len(span.GetName()) == 0 || strings.TrimSpace(span.GetName()) == "" {
		return oops.E(oops.CodeBadRequest, nil, "span name is empty")
	}
	if span.GetStartTimeUnixNano() == 0 {
		return oops.E(oops.CodeBadRequest, nil, "span start_time_unix_nano is zero")
	}
	if span.GetEndTimeUnixNano() == 0 {
		return oops.E(oops.CodeBadRequest, nil, "span end_time_unix_nano is zero")
	}
	if span.GetEndTimeUnixNano() < span.GetStartTimeUnixNano() {
		return oops.E(oops.CodeBadRequest, nil, "span end_time_unix_nano is before start_time_unix_nano")
	}
	return nil
}
