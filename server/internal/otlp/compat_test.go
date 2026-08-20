// Package otlp holds the guarantee that gram.otel.v1 messages are
// wire-compatible supersets of their opentelemetry.proto counterparts.
//
// The gram.otel.v1 protos deliberately reuse OTLP's exact field numbers and
// scalar types so bytes produced by one unmarshal cleanly into the other, and
// Gram-only fields live at 1000+ where an OTLP parser skips them as unknown
// fields. Nothing in the proto files enforces that; these tests do. If someone
// renumbers a field or swaps fixed64 for int64, this is what fails.
package otlp

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
	otlpcommon "go.opentelemetry.io/proto/otlp/common/v1"
	otlplogs "go.opentelemetry.io/proto/otlp/logs/v1"
	otlptrace "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

// logAttrFixtures covers every AnyValue variant, including the recursive ones,
// so a regression in any oneof arm surfaces here.
func logAttrFixtures() []*otelv1.LogRecord_KeyValue {
	return []*otelv1.LogRecord_KeyValue{
		(&otelv1.LogRecord_KeyValue_builder{
			Key:   new("str"),
			Value: (&otelv1.LogRecord_AnyValue_builder{StringValue: new("v")}).Build(),
		}).Build(),
		(&otelv1.LogRecord_KeyValue_builder{
			Key:   new("int"),
			Value: (&otelv1.LogRecord_AnyValue_builder{IntValue: new(int64(1 << 60))}).Build(),
		}).Build(),
		(&otelv1.LogRecord_KeyValue_builder{
			Key:   new("dbl"),
			Value: (&otelv1.LogRecord_AnyValue_builder{DoubleValue: new(1.5)}).Build(),
		}).Build(),
		(&otelv1.LogRecord_KeyValue_builder{
			Key:   new("bool"),
			Value: (&otelv1.LogRecord_AnyValue_builder{BoolValue: new(true)}).Build(),
		}).Build(),
		(&otelv1.LogRecord_KeyValue_builder{
			Key:   new("bytes"),
			Value: (&otelv1.LogRecord_AnyValue_builder{BytesValue: []byte{0xde, 0xad}}).Build(),
		}).Build(),
		(&otelv1.LogRecord_KeyValue_builder{
			Key: new("arr"),
			Value: (&otelv1.LogRecord_AnyValue_builder{
				ArrayValue: (&otelv1.LogRecord_ArrayValue_builder{
					Values: []*otelv1.LogRecord_AnyValue{
						(&otelv1.LogRecord_AnyValue_builder{IntValue: new(int64(7))}).Build(),
						(&otelv1.LogRecord_AnyValue_builder{StringValue: new("mixed")}).Build(),
					},
				}).Build(),
			}).Build(),
		}).Build(),
		(&otelv1.LogRecord_KeyValue_builder{
			Key: new("nested"),
			Value: (&otelv1.LogRecord_AnyValue_builder{
				KvlistValue: (&otelv1.LogRecord_KeyValueList_builder{
					Values: []*otelv1.LogRecord_KeyValue{
						(&otelv1.LogRecord_KeyValue_builder{
							Key:   new("inner"),
							Value: (&otelv1.LogRecord_AnyValue_builder{BoolValue: new(true)}).Build(),
						}).Build(),
					},
				}).Build(),
			}).Build(),
		}).Build(),
	}
}

func TestLogRecordUnmarshalsAsOTLP(t *testing.T) {
	t.Parallel()

	src := (&otelv1.LogRecord_builder{
		TimeUnixNano:           new(uint64(1700000000000000001)),
		ObservedTimeUnixNano:   new(uint64(1700000000000000002)),
		SeverityNumber:         otelv1.LogRecord_SEVERITY_NUMBER_ERROR.Enum(),
		SeverityText:           new("ERROR"),
		Body:                   (&otelv1.LogRecord_AnyValue_builder{StringValue: new("boom")}).Build(),
		Attributes:             logAttrFixtures(),
		DroppedAttributesCount: new(uint32(3)),
		Flags:                  new(uint32(1)),
		TraceId:                []byte("0123456789abcdef"),
		SpanId:                 []byte("01234567"),
		EventName:              new("some.event"),
		// Gram-only fields at 1000+. An OTLP parser must ignore these rather
		// than mistake them for one of its own.
		RecordId: new("dedupe-key"),
		Provenance: (&otelv1.LogRecord_Provenance_builder{
			Source:         new("mcp-gateway"),
			OrganizationId: new("org"),
			ProjectId:      new("proj"),
		}).Build(),
		Scope: (&otelv1.LogRecord_InstrumentationScope_builder{
			Name:    new("scope"),
			Version: new("1.2.3"),
		}).Build(),
		ScopeSchemaUrl: new("https://opentelemetry.io/schemas/1.27.0"),
		Resource: (&otelv1.LogRecord_Resource_builder{
			Attributes: []*otelv1.LogRecord_KeyValue{
				(&otelv1.LogRecord_KeyValue_builder{
					Key:   new("service.name"),
					Value: (&otelv1.LogRecord_AnyValue_builder{StringValue: new("svc")}).Build(),
				}).Build(),
			},
		}).Build(),
		ResourceSchemaUrl: new("https://opentelemetry.io/schemas/1.27.0"),
	}).Build()

	raw, err := proto.Marshal(src)
	require.NoError(t, err, "marshal canonical gram log record")

	for _, test := range logCopies() {
		gramRecord := test.newRecord()
		require.NoError(t, proto.Unmarshal(raw, gramRecord), "%s: unmarshal canonical gram log record", test.name)

		copyRaw, err := proto.Marshal(gramRecord)
		require.NoError(t, err, "%s: marshal gram log record", test.name)

		var got otlplogs.LogRecord
		require.NoError(t, proto.Unmarshal(copyRaw, &got), "%s: unmarshal as OTLP log record", test.name)

		require.Equal(t, uint64(1700000000000000001), got.GetTimeUnixNano(), test.name)
		require.Equal(t, uint64(1700000000000000002), got.GetObservedTimeUnixNano(), test.name)
		require.Equal(t, otlplogs.SeverityNumber_SEVERITY_NUMBER_ERROR, got.GetSeverityNumber(), test.name)
		require.Equal(t, "ERROR", got.GetSeverityText(), test.name)
		require.Equal(t, "boom", got.GetBody().GetStringValue(), test.name)
		require.Equal(t, uint32(3), got.GetDroppedAttributesCount(), test.name)
		require.Equal(t, uint32(1), got.GetFlags(), test.name)
		require.Equal(t, []byte("0123456789abcdef"), got.GetTraceId(), test.name)
		require.Equal(t, []byte("01234567"), got.GetSpanId(), test.name)
		require.Equal(t, "some.event", got.GetEventName(), test.name)

		requireAttrsSurvived(t, got.GetAttributes())
	}
}

// requireAttrsSurvived checks that every AnyValue variant crossed with its type
// tag intact — the property that carrying typed attributes exists to provide.
func requireAttrsSurvived(t *testing.T, attrs []*otlpcommon.KeyValue) {
	t.Helper()

	require.Len(t, attrs, 7)

	byKey := make(map[string]*otlpcommon.AnyValue, len(attrs))
	for _, kv := range attrs {
		byKey[kv.GetKey()] = kv.GetValue()
	}

	require.Equal(t, "v", byKey["str"].GetStringValue())
	require.Equal(t, int64(1)<<60, byKey["int"].GetIntValue())
	require.IsType(t, &otlpcommon.AnyValue_IntValue{}, byKey["int"].GetValue(), "int attribute lost its type tag")
	require.InDelta(t, 1.5, byKey["dbl"].GetDoubleValue(), 0)
	require.IsType(t, &otlpcommon.AnyValue_DoubleValue{}, byKey["dbl"].GetValue(), "double attribute lost its type tag")
	require.True(t, byKey["bool"].GetBoolValue())
	require.Equal(t, []byte{0xde, 0xad}, byKey["bytes"].GetBytesValue())
	require.IsType(t, &otlpcommon.AnyValue_BytesValue{}, byKey["bytes"].GetValue(), "bytes attribute lost its type tag")

	arr := byKey["arr"].GetArrayValue().GetValues()
	require.Len(t, arr, 2)
	require.Equal(t, int64(7), arr[0].GetIntValue())
	require.Equal(t, "mixed", arr[1].GetStringValue())

	kvs := byKey["nested"].GetKvlistValue().GetValues()
	require.Len(t, kvs, 1)
	require.Equal(t, "inner", kvs[0].GetKey())
	require.True(t, kvs[0].GetValue().GetBoolValue())
}

func TestGramSpansUnmarshalAsOTLP(t *testing.T) {
	t.Parallel()

	src := (&otelv1.Span_builder{
		TraceId:           []byte("0123456789abcdef"),
		SpanId:            []byte("01234567"),
		TraceState:        new("vendor=v"),
		ParentSpanId:      []byte("76543210"),
		Name:              new("GET /x"),
		Kind:              otelv1.Span_SPAN_KIND_SERVER.Enum(),
		StartTimeUnixNano: new(uint64(1700000000000000001)),
		EndTimeUnixNano:   new(uint64(1700000000000000999)),
		Attributes: []*otelv1.Span_KeyValue{
			(&otelv1.Span_KeyValue_builder{
				Key:   new("http.response.status_code"),
				Value: (&otelv1.Span_AnyValue_builder{IntValue: new(int64(503))}).Build(),
			}).Build(),
		},
		DroppedAttributesCount: new(uint32(1)),
		DroppedEventsCount:     new(uint32(2)),
		DroppedLinksCount:      new(uint32(3)),
		Flags:                  new(uint32(256)),
		Status: (&otelv1.Span_Status_builder{
			Code:    otelv1.Span_STATUS_CODE_ERROR.Enum(),
			Message: new("nope"),
		}).Build(),
		Events: []*otelv1.Span_Event{
			(&otelv1.Span_Event_builder{
				TimeUnixNano: new(uint64(1700000000000000500)),
				Name:         new("exception"),
			}).Build(),
		},
		Links: []*otelv1.Span_Link{
			(&otelv1.Span_Link_builder{
				TraceId:    []byte("fedcba9876543210"),
				SpanId:     []byte("76543210"),
				TraceState: new("other=o"),
				Flags:      new(uint32(1)),
			}).Build(),
		},
		// Gram-only fields at 1000+.
		Scope: (&otelv1.Span_InstrumentationScope_builder{Name: new("scope")}).Build(),
		Provenance: (&otelv1.Span_Provenance_builder{
			Source:         new("risk"),
			OrganizationId: new("org"),
			ProjectId:      new("proj"),
		}).Build(),
	}).Build()

	raw, err := proto.Marshal(src)
	require.NoError(t, err, "marshal canonical gram span")

	for _, test := range spanCopies() {
		gramSpan := test.newSpan()
		require.NoError(t, proto.Unmarshal(raw, gramSpan), "%s: unmarshal canonical gram span", test.name)

		copyRaw, err := proto.Marshal(gramSpan)
		require.NoError(t, err, "%s: marshal gram span", test.name)

		var got otlptrace.Span
		require.NoError(t, proto.Unmarshal(copyRaw, &got), "%s: unmarshal as OTLP span", test.name)

		require.Equal(t, []byte("0123456789abcdef"), got.GetTraceId())
		require.Equal(t, []byte("01234567"), got.GetSpanId())
		require.Equal(t, []byte("76543210"), got.GetParentSpanId())
		require.Equal(t, "vendor=v", got.GetTraceState())
		require.Equal(t, "GET /x", got.GetName())
		require.Equal(t, otlptrace.Span_SPAN_KIND_SERVER, got.GetKind())
		require.Equal(t, uint64(1700000000000000001), got.GetStartTimeUnixNano())
		require.Equal(t, uint64(1700000000000000999), got.GetEndTimeUnixNano())
		require.Equal(t, uint32(256), got.GetFlags())
		require.Equal(t, uint32(1), got.GetDroppedAttributesCount())
		require.Equal(t, uint32(2), got.GetDroppedEventsCount())
		require.Equal(t, uint32(3), got.GetDroppedLinksCount())

		require.Equal(t, otlptrace.Status_STATUS_CODE_ERROR, got.GetStatus().GetCode())
		require.Equal(t, "nope", got.GetStatus().GetMessage())

		require.Len(t, got.GetAttributes(), 1)
		require.Equal(t, "http.response.status_code", got.GetAttributes()[0].GetKey())
		require.Equal(t, int64(503), got.GetAttributes()[0].GetValue().GetIntValue())

		require.Len(t, got.GetEvents(), 1)
		require.Equal(t, "exception", got.GetEvents()[0].GetName())
		require.Equal(t, uint64(1700000000000000500), got.GetEvents()[0].GetTimeUnixNano())

		require.Len(t, got.GetLinks(), 1)
		require.Equal(t, []byte("fedcba9876543210"), got.GetLinks()[0].GetTraceId())
		require.Equal(t, "other=o", got.GetLinks()[0].GetTraceState())
		require.Equal(t, uint32(1), got.GetLinks()[0].GetFlags())
	}
}

// TestOTLPSpanUnmarshalsAsGramCopies covers the reverse direction: bytes from
// an upstream OTLP producer parse into both Gram span messages.
func TestOTLPSpanUnmarshalsAsGramCopies(t *testing.T) {
	t.Parallel()

	src := &otlptrace.Span{
		TraceId:           []byte("0123456789abcdef"),
		SpanId:            []byte("01234567"),
		ParentSpanId:      []byte("76543210"),
		TraceState:        "vendor=v",
		Name:              "GET /x",
		Kind:              otlptrace.Span_SPAN_KIND_CLIENT,
		StartTimeUnixNano: 1700000000000000001,
		EndTimeUnixNano:   1700000000000000999,
		Flags:             256,
		Status:            &otlptrace.Status{Code: otlptrace.Status_STATUS_CODE_OK},
		Attributes: []*otlpcommon.KeyValue{{
			Key:   "http.response.status_code",
			Value: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_IntValue{IntValue: 200}},
		}},
	}

	raw, err := proto.Marshal(src)
	require.NoError(t, err, "marshal OTLP span")

	for _, test := range spanCopies() {
		gramSpan := test.newSpan()
		require.NoError(t, proto.Unmarshal(raw, gramSpan), "%s: unmarshal as gram span", test.name)

		copyRaw, err := proto.Marshal(gramSpan)
		require.NoError(t, err, "%s: re-marshal gram span", test.name)

		var got otlptrace.Span
		require.NoError(t, proto.Unmarshal(copyRaw, &got), "%s: unmarshal round-tripped bytes as OTLP span", test.name)

		require.Equal(t, []byte("0123456789abcdef"), got.GetTraceId(), test.name)
		require.Equal(t, []byte("01234567"), got.GetSpanId(), test.name)
		require.Equal(t, []byte("76543210"), got.GetParentSpanId(), test.name)
		require.Equal(t, "vendor=v", got.GetTraceState(), test.name)
		require.Equal(t, "GET /x", got.GetName(), test.name)
		require.Equal(t, otlptrace.Span_SPAN_KIND_CLIENT, got.GetKind(), test.name)
		require.Equal(t, uint64(1700000000000000001), got.GetStartTimeUnixNano(), test.name)
		require.Equal(t, uint64(1700000000000000999), got.GetEndTimeUnixNano(), test.name)
		require.Equal(t, uint32(256), got.GetFlags(), test.name)
		require.Equal(t, otlptrace.Status_STATUS_CODE_OK, got.GetStatus().GetCode(), test.name)

		require.Len(t, got.GetAttributes(), 1, test.name)
		require.Equal(t, "http.response.status_code", got.GetAttributes()[0].GetKey(), test.name)
		require.Equal(t, int64(200), got.GetAttributes()[0].GetValue().GetIntValue(), test.name)
	}
}

// TestOTLPLogRecordUnmarshalsAsGram covers the reverse direction for logs.
func TestOTLPLogRecordUnmarshalsAsGram(t *testing.T) {
	t.Parallel()

	src := &otlplogs.LogRecord{
		TimeUnixNano:         1700000000000000001,
		ObservedTimeUnixNano: 1700000000000000002,
		SeverityNumber:       otlplogs.SeverityNumber_SEVERITY_NUMBER_WARN,
		SeverityText:         "WARN",
		Body:                 &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: "hi"}},
		Flags:                1,
		TraceId:              []byte("0123456789abcdef"),
		SpanId:               []byte("01234567"),
		EventName:            "some.event",
	}

	raw, err := proto.Marshal(src)
	require.NoError(t, err, "marshal OTLP log record")

	for _, test := range logCopies() {
		gramRecord := test.newRecord()
		require.NoError(t, proto.Unmarshal(raw, gramRecord), "%s: unmarshal as gram log record", test.name)

		copyRaw, err := proto.Marshal(gramRecord)
		require.NoError(t, err, "%s: marshal gram log record", test.name)

		var got otlplogs.LogRecord
		require.NoError(t, proto.Unmarshal(copyRaw, &got), "%s: unmarshal round-tripped bytes as OTLP log record", test.name)
		require.Equal(t, uint64(1700000000000000001), got.GetTimeUnixNano(), test.name)
		require.Equal(t, uint64(1700000000000000002), got.GetObservedTimeUnixNano(), test.name)
		require.Equal(t, otlplogs.SeverityNumber_SEVERITY_NUMBER_WARN, got.GetSeverityNumber(), test.name)
		require.Equal(t, "WARN", got.GetSeverityText(), test.name)
		require.Equal(t, "hi", got.GetBody().GetStringValue(), test.name)
		require.Equal(t, uint32(1), got.GetFlags(), test.name)
		require.Equal(t, []byte("0123456789abcdef"), got.GetTraceId(), test.name)
		require.Equal(t, []byte("01234567"), got.GetSpanId(), test.name)
		require.Equal(t, "some.event", got.GetEventName(), test.name)
	}
}

// TestGramSpanFieldsSurviveOTLPRoundTrip confirms Gram-only fields at 1000+ are
// preserved for both Gram span messages when a message passes through an OTLP
// parser, so a relay that decodes and re-encodes does not silently drop
// tenancy.
func TestGramSpanFieldsSurviveOTLPRoundTrip(t *testing.T) {
	t.Parallel()

	src := (&otelv1.Span_builder{
		TraceId:    []byte("0123456789abcdef"),
		SpanId:     []byte("01234567"),
		Name:       new("x"),
		Provenance: (&otelv1.Span_Provenance_builder{ProjectId: new("proj")}).Build(),
		Scope:      (&otelv1.Span_InstrumentationScope_builder{Name: new("scope")}).Build(),
	}).Build()

	raw, err := proto.Marshal(src)
	require.NoError(t, err, "marshal canonical gram span")

	for _, test := range spanCopies() {
		gramSpan := test.newSpan()
		require.NoError(t, proto.Unmarshal(raw, gramSpan), "%s: unmarshal canonical gram span", test.name)

		copyRaw, err := proto.Marshal(gramSpan)
		require.NoError(t, err, "%s: marshal gram span", test.name)

		var viaOTLP otlptrace.Span
		require.NoError(t, proto.Unmarshal(copyRaw, &viaOTLP), "%s: unmarshal as OTLP span", test.name)

		reencoded, err := proto.Marshal(&viaOTLP)
		require.NoError(t, err, "%s: re-marshal OTLP span", test.name)

		back := test.newSpan()
		require.NoError(t, proto.Unmarshal(reencoded, back), "%s: unmarshal round-tripped bytes as gram span", test.name)

		switch got := back.(type) {
		case *otelv1.Span:
			require.Equal(t, "proj", got.GetProvenance().GetProjectId(), "Span: provenance.project_id lost in OTLP round trip")
			require.Equal(t, "scope", got.GetScope().GetName(), "Span: scope.name lost in OTLP round trip")
		case *otelv1.InboundSpan:
			require.Equal(t, "proj", got.GetProvenance().GetProjectId(), "InboundSpan: provenance.project_id lost in OTLP round trip")
			require.Equal(t, "scope", got.GetScope().GetName(), "InboundSpan: scope.name lost in OTLP round trip")
		default:
			require.Failf(t, "unexpected gram span type", "%T", got)
		}
	}
}

// TestGramLogRecordFieldsSurviveOTLPRoundTrip is the log-side counterpart, and
// additionally covers record_id, which has no span equivalent.
func TestGramLogRecordFieldsSurviveOTLPRoundTrip(t *testing.T) {
	t.Parallel()

	src := (&otelv1.LogRecord_builder{
		TimeUnixNano: new(uint64(1700000000000000001)),
		Body:         (&otelv1.LogRecord_AnyValue_builder{StringValue: new("boom")}).Build(),
		RecordId:     new("dedupe-key"),
		Provenance: (&otelv1.LogRecord_Provenance_builder{
			Source:         new("mcp-gateway"),
			OrganizationId: new("org"),
			ProjectId:      new("proj"),
		}).Build(),
		Scope:          (&otelv1.LogRecord_InstrumentationScope_builder{Name: new("scope")}).Build(),
		ScopeSchemaUrl: new("https://opentelemetry.io/schemas/1.27.0"),
		Resource: (&otelv1.LogRecord_Resource_builder{
			Attributes: []*otelv1.LogRecord_KeyValue{
				(&otelv1.LogRecord_KeyValue_builder{
					Key:   new("service.name"),
					Value: (&otelv1.LogRecord_AnyValue_builder{StringValue: new("svc")}).Build(),
				}).Build(),
			},
		}).Build(),
		ResourceSchemaUrl: new("https://opentelemetry.io/schemas/1.28.0"),
	}).Build()

	raw, err := proto.Marshal(src)
	require.NoError(t, err, "marshal gram log record")

	var viaOTLP otlplogs.LogRecord
	require.NoError(t, proto.Unmarshal(raw, &viaOTLP), "unmarshal as OTLP log record")

	reencoded, err := proto.Marshal(&viaOTLP)
	require.NoError(t, err, "re-marshal OTLP log record")

	var back otelv1.LogRecord
	require.NoError(t, proto.Unmarshal(reencoded, &back), "unmarshal round-tripped bytes as gram log record")

	require.Equal(t, "dedupe-key", back.GetRecordId(), "record_id lost in OTLP round trip")
	require.Equal(t, "mcp-gateway", back.GetProvenance().GetSource(), "provenance.source lost in OTLP round trip")
	require.Equal(t, "org", back.GetProvenance().GetOrganizationId(), "provenance.organization_id lost in OTLP round trip")
	require.Equal(t, "proj", back.GetProvenance().GetProjectId(), "provenance.project_id lost in OTLP round trip")
	require.Equal(t, "scope", back.GetScope().GetName(), "scope.name lost in OTLP round trip")
	require.Equal(t, "https://opentelemetry.io/schemas/1.27.0", back.GetScopeSchemaUrl(), "scope_schema_url lost in OTLP round trip")
	require.Equal(t, "https://opentelemetry.io/schemas/1.28.0", back.GetResourceSchemaUrl(), "resource_schema_url lost in OTLP round trip")

	resAttrs := back.GetResource().GetAttributes()
	require.Len(t, resAttrs, 1, "resource lost in OTLP round trip")
	require.Equal(t, "service.name", resAttrs[0].GetKey())
	require.Equal(t, "svc", resAttrs[0].GetValue().GetStringValue())
}
