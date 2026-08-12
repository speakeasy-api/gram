package otlp

import (
	"fmt"
	"strings"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
	otlpcommon "go.opentelemetry.io/proto/otlp/common/v1"
	otlplogs "go.opentelemetry.io/proto/otlp/logs/v1"
	otlpresource "go.opentelemetry.io/proto/otlp/resource/v1"
	otlptrace "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// messagePair binds one upstream OTLP message to the gram.otel.v1 copy that is
// meant to be a wire-compatible superset of it.
type messagePair struct {
	// name labels the pair in failure output.
	name string
	// upstream is the opentelemetry.proto message that defines the contract.
	upstream proto.Message
	// ours is the gram.otel.v1 copy that must cover it.
	ours proto.Message
	// skip lists upstream field numbers deliberately not carried, keyed by
	// number with the reason as the value. Every entry is a conscious decision
	// to diverge; an unlisted absence is drift.
	skip map[protoreflect.FieldNumber]string
}

// entityRefsSkip omits Resource.entity_refs: Development status upstream, so
// it is not carried and its field number is reserved in our copies.
var entityRefsSkip = map[protoreflect.FieldNumber]string{
	3: "Resource.entity_refs is Development status upstream",
}

// strIndexSkip omits AnyValue.string_value_strindex, which indexes into a
// ProfilesDictionary string table. It is Alpha and used exclusively by the
// Profiling signal; the spec directs receivers of other signals to treat its
// presence as non-fatal and process the value as absent. Carrying it would mean
// carrying the dictionary that gives it meaning, which logs and traces do not
// have.
var strIndexSkip = map[protoreflect.FieldNumber]string{
	8: "AnyValue.string_value_strindex is Alpha and Profiling-signal only",
}

// keyStrIndexSkip omits KeyValue.key_strindex for the same reason as
// strIndexSkip: it indexes into the Profiling signal's string table, which logs
// and traces do not carry.
var keyStrIndexSkip = map[protoreflect.FieldNumber]string{
	3: "KeyValue.key_strindex is Alpha and Profiling-signal only",
}

// messagePairs enumerates every gram.otel.v1 message that copies an upstream
// type. The common.v1 types are copied once per top-level message (a Pub/Sub
// schema must be a single self-contained file), so each copy is checked.
func messagePairs() []messagePair {
	return []messagePair{
		{name: "logs.v1.LogRecord", upstream: &otlplogs.LogRecord{}, ours: &otelv1.LogRecord{}},
		{name: "trace.v1.Span", upstream: &otlptrace.Span{}, ours: &otelv1.Span{}},
		{name: "trace.v1.Span.Event", upstream: &otlptrace.Span_Event{}, ours: &otelv1.Span_Event{}},
		{name: "trace.v1.Span.Link", upstream: &otlptrace.Span_Link{}, ours: &otelv1.Span_Link{}},
		{name: "trace.v1.Status", upstream: &otlptrace.Status{}, ours: &otelv1.Span_Status{}},

		{name: "resource.v1.Resource (LogRecord)", upstream: &otlpresource.Resource{}, ours: &otelv1.LogRecord_Resource{}, skip: entityRefsSkip},
		{name: "resource.v1.Resource (Span)", upstream: &otlpresource.Resource{}, ours: &otelv1.Span_Resource{}, skip: entityRefsSkip},

		{name: "common.v1.InstrumentationScope (LogRecord)", upstream: &otlpcommon.InstrumentationScope{}, ours: &otelv1.LogRecord_InstrumentationScope{}},
		{name: "common.v1.InstrumentationScope (Span)", upstream: &otlpcommon.InstrumentationScope{}, ours: &otelv1.Span_InstrumentationScope{}},

		{name: "common.v1.AnyValue (LogRecord)", upstream: &otlpcommon.AnyValue{}, ours: &otelv1.LogRecord_AnyValue{}, skip: strIndexSkip},
		{name: "common.v1.AnyValue (Span)", upstream: &otlpcommon.AnyValue{}, ours: &otelv1.Span_AnyValue{}, skip: strIndexSkip},
		{name: "common.v1.ArrayValue (LogRecord)", upstream: &otlpcommon.ArrayValue{}, ours: &otelv1.LogRecord_ArrayValue{}},
		{name: "common.v1.ArrayValue (Span)", upstream: &otlpcommon.ArrayValue{}, ours: &otelv1.Span_ArrayValue{}},
		{name: "common.v1.KeyValueList (LogRecord)", upstream: &otlpcommon.KeyValueList{}, ours: &otelv1.LogRecord_KeyValueList{}},
		{name: "common.v1.KeyValueList (Span)", upstream: &otlpcommon.KeyValueList{}, ours: &otelv1.Span_KeyValueList{}},
		{name: "common.v1.KeyValue (LogRecord)", upstream: &otlpcommon.KeyValue{}, ours: &otelv1.LogRecord_KeyValue{}, skip: keyStrIndexSkip},
		{name: "common.v1.KeyValue (Span)", upstream: &otlpcommon.KeyValue{}, ours: &otelv1.Span_KeyValue{}, skip: keyStrIndexSkip},
	}
}

// TestNoUpstreamFieldsMissing walks every field upstream declares and asserts
// our copy carries it at the same number, kind and cardinality.
//
// This is the drift guard. The wire-compatibility tests in compat_test.go check
// the fields we already have; they cannot notice a field OTLP *added*. Bumping
// go.opentelemetry.io/proto/otlp therefore fails here with the exact field that
// appeared, at the moment mirroring it is cheap — rather than the omission
// being discovered much later by a downstream consumer.
//
// Fixing a failure means adding the field to the proto at the number and type
// upstream uses, or recording it in the pair's skip map with a reason.
func TestNoUpstreamFieldsMissing(t *testing.T) {
	t.Parallel()

	// Collect every divergence before failing. A bump that adds several fields
	// should report all of them in one run rather than one per invocation.
	var problems []string

	for _, pair := range messagePairs() {
		upstream := pair.upstream.ProtoReflect().Descriptor()
		ours := pair.ours.ProtoReflect().Descriptor()

		for i := range upstream.Fields().Len() {
			want := upstream.Fields().Get(i)

			if reason, skipped := pair.skip[want.Number()]; skipped {
				if ours.Fields().ByNumber(want.Number()) != nil {
					problems = append(problems, fmt.Sprintf(
						"%s: field %d (%s) is listed as deliberately omitted (%s) but our copy declares it — drop the skip entry",
						pair.name, want.Number(), want.Name(), reason))
				}
				continue
			}

			got := ours.Fields().ByNumber(want.Number())
			if got == nil {
				problems = append(problems, fmt.Sprintf(
					"%s: upstream field %d (%s, %s %s) has no counterpart in %s — OTLP added a field we do not carry",
					pair.name, want.Number(), want.Name(), want.Cardinality(), want.Kind(), ours.FullName()))
				continue
			}

			if want.Kind() != got.Kind() {
				problems = append(problems, fmt.Sprintf(
					"%s: field %d (%s) kind is %s upstream but %s here — wire format would not match",
					pair.name, want.Number(), want.Name(), want.Kind(), got.Kind()))
			}
			if want.Cardinality() != got.Cardinality() {
				problems = append(problems, fmt.Sprintf(
					"%s: field %d (%s) cardinality is %s upstream but %s here — wire format would not match",
					pair.name, want.Number(), want.Name(), want.Cardinality(), got.Cardinality()))
			}
		}
	}

	require.Empty(t, problems,
		"gram.otel.v1 has drifted from opentelemetry.proto:\n%s", strings.Join(problems, "\n"))
}

// TestGramAdditionsStayOutOfUpstreamRange asserts every field we add sits at
// 1000 or above, leaving the whole low range to OTLP.
//
// Without this, a Gram field could be added at, say, 17 on Span — harmless
// today, and silently wire-incompatible the day OTLP allocates that number.
// Keeping the ranges disjoint is what makes the superset claim durable rather
// than true-for-now.
func TestGramAdditionsStayOutOfUpstreamRange(t *testing.T) {
	t.Parallel()

	const gramFieldFloor = 1000

	for _, pair := range messagePairs() {
		upstream := pair.upstream.ProtoReflect().Descriptor()
		ours := pair.ours.ProtoReflect().Descriptor()

		upstreamNumbers := make(map[protoreflect.FieldNumber]struct{}, upstream.Fields().Len())
		for i := range upstream.Fields().Len() {
			upstreamNumbers[upstream.Fields().Get(i).Number()] = struct{}{}
		}

		for i := range ours.Fields().Len() {
			got := ours.Fields().Get(i)
			if _, shared := upstreamNumbers[got.Number()]; shared {
				continue
			}

			require.GreaterOrEqual(t, int(got.Number()), gramFieldFloor,
				"%s: %s is not an upstream field but sits at %d, inside the range OTLP allocates from — move it to %d+",
				pair.name, got.Name(), got.Number(), gramFieldFloor)
		}
	}
}

// TestUpstreamEnumsMatch asserts our enum copies carry every upstream value at
// the same number. Enum values go on the wire as their number, so a missing or
// renumbered entry is a decode bug, and a value OTLP adds (as it did across
// severity levels) would otherwise pass unnoticed.
func TestUpstreamEnumsMatch(t *testing.T) {
	t.Parallel()

	enumPairs := []struct {
		name     string
		upstream protoreflect.EnumDescriptor
		ours     protoreflect.EnumDescriptor
	}{
		{
			name:     "logs.v1.SeverityNumber",
			upstream: otlplogs.SeverityNumber(0).Descriptor(),
			ours:     otelv1.LogRecord_SEVERITY_NUMBER_UNSPECIFIED.Descriptor(),
		},
		{
			name:     "trace.v1.Span.SpanKind",
			upstream: otlptrace.Span_SPAN_KIND_UNSPECIFIED.Descriptor(),
			ours:     otelv1.Span_SPAN_KIND_UNSPECIFIED.Descriptor(),
		},
		{
			name:     "trace.v1.Status.StatusCode",
			upstream: otlptrace.Status_STATUS_CODE_UNSET.Descriptor(),
			ours:     otelv1.Span_STATUS_CODE_UNSPECIFIED.Descriptor(),
		},
	}

	for _, pair := range enumPairs {
		ourNumbers := make(map[protoreflect.EnumNumber]string, pair.ours.Values().Len())
		for i := range pair.ours.Values().Len() {
			v := pair.ours.Values().Get(i)
			ourNumbers[v.Number()] = string(v.Name())
		}

		for i := range pair.upstream.Values().Len() {
			want := pair.upstream.Values().Get(i)
			_, ok := ourNumbers[want.Number()]
			require.True(t, ok,
				"%s: upstream value %s = %d has no counterpart — OTLP added an enum value we do not carry",
				pair.name, want.Name(), want.Number())
		}
	}
}

// TestSkipEntriesAreReserved asserts a deliberately omitted upstream field has
// its number reserved in our copy, so it cannot later be handed to an unrelated
// Gram field and quietly collide with upstream's meaning.
func TestSkipEntriesAreReserved(t *testing.T) {
	t.Parallel()

	for _, pair := range messagePairs() {
		if len(pair.skip) == 0 {
			continue
		}

		ours := pair.ours.ProtoReflect().Descriptor()

		for number, reason := range pair.skip {
			require.True(t, reservedNumber(ours, number),
				"%s: field %d is skipped (%s) but not reserved in %s",
				pair.name, number, reason, ours.FullName())
		}
	}
}

func reservedNumber(desc protoreflect.MessageDescriptor, number protoreflect.FieldNumber) bool {
	ranges := desc.ReservedRanges()
	for i := range ranges.Len() {
		r := ranges.Get(i)
		if number >= r[0] && number < r[1] {
			return true
		}
	}
	return false
}
