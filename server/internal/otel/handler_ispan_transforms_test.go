package otel

import (
	"testing"

	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/otel/enrich"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/mock"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
	otelattr "go.opentelemetry.io/otel/attribute"
)

func TestApplySpanEnrichments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		enrichment otelattr.KeyValue
		check      func(*testing.T, *otelv1.Span_AnyValue)
	}{
		{
			name:       "bool",
			enrichment: otelattr.Bool("bool", true),
			check: func(t *testing.T, got *otelv1.Span_AnyValue) {
				t.Helper()
				require.True(t, got.GetBoolValue())
			},
		},
		{
			name:       "int64",
			enrichment: otelattr.Int64("int64", 42),
			check: func(t *testing.T, got *otelv1.Span_AnyValue) {
				t.Helper()
				require.Equal(t, int64(42), got.GetIntValue())
			},
		},
		{
			name:       "float64",
			enrichment: otelattr.Float64("float64", 1.5),
			check: func(t *testing.T, got *otelv1.Span_AnyValue) {
				t.Helper()
				require.InDelta(t, 1.5, got.GetDoubleValue(), 0)
			},
		},
		{
			name:       "string",
			enrichment: otelattr.String("string", "value"),
			check: func(t *testing.T, got *otelv1.Span_AnyValue) {
				t.Helper()
				require.Equal(t, "value", got.GetStringValue())
			},
		},
		{
			name:       "byte slice",
			enrichment: otelattr.ByteSlice("byte slice", []byte{0xde, 0xad}),
			check: func(t *testing.T, got *otelv1.Span_AnyValue) {
				t.Helper()
				require.Equal(t, []byte{0xde, 0xad}, got.GetBytesValue())
			},
		},
		{
			name:       "bool slice",
			enrichment: otelattr.BoolSlice("bool slice", []bool{true, false}),
			check: func(t *testing.T, got *otelv1.Span_AnyValue) {
				t.Helper()
				values := got.GetArrayValue().GetValues()
				require.Len(t, values, 2)
				require.True(t, values[0].GetBoolValue())
				require.False(t, values[1].GetBoolValue())
			},
		},
		{
			name:       "int64 slice",
			enrichment: otelattr.Int64Slice("int64 slice", []int64{1, 2}),
			check: func(t *testing.T, got *otelv1.Span_AnyValue) {
				t.Helper()
				values := got.GetArrayValue().GetValues()
				require.Len(t, values, 2)
				require.Equal(t, int64(1), values[0].GetIntValue())
				require.Equal(t, int64(2), values[1].GetIntValue())
			},
		},
		{
			name:       "float64 slice",
			enrichment: otelattr.Float64Slice("float64 slice", []float64{1.5, 2.5}),
			check: func(t *testing.T, got *otelv1.Span_AnyValue) {
				t.Helper()
				values := got.GetArrayValue().GetValues()
				require.Len(t, values, 2)
				require.InDelta(t, 1.5, values[0].GetDoubleValue(), 0)
				require.InDelta(t, 2.5, values[1].GetDoubleValue(), 0)
			},
		},
		{
			name:       "string slice",
			enrichment: otelattr.StringSlice("string slice", []string{"a", "b"}),
			check: func(t *testing.T, got *otelv1.Span_AnyValue) {
				t.Helper()
				values := got.GetArrayValue().GetValues()
				require.Len(t, values, 2)
				require.Equal(t, "a", values[0].GetStringValue())
				require.Equal(t, "b", values[1].GetStringValue())
			},
		},
		{
			name: "heterogeneous slice",
			enrichment: otelattr.Slice(
				"heterogeneous slice",
				otelattr.StringValue("text"),
				otelattr.Int64Value(42),
				otelattr.SliceValue(
					otelattr.BoolValue(true),
					otelattr.ByteSliceValue([]byte{0xde, 0xad}),
				),
			),
			check: func(t *testing.T, got *otelv1.Span_AnyValue) {
				t.Helper()
				values := got.GetArrayValue().GetValues()
				require.Len(t, values, 3)
				require.Equal(t, "text", values[0].GetStringValue())
				require.Equal(t, int64(42), values[1].GetIntValue())
				nested := values[2].GetArrayValue().GetValues()
				require.Len(t, nested, 2)
				require.True(t, nested[0].GetBoolValue())
				require.Equal(t, []byte{0xde, 0xad}, nested[1].GetBytesValue())
			},
		},
	}

	out := (&otelv1.Span_builder{
		Attributes: []*otelv1.Span_KeyValue{
			(&otelv1.Span_KeyValue_builder{
				Key:   new("existing"),
				Value: (&otelv1.Span_AnyValue_builder{StringValue: new("preserved")}).Build(),
			}).Build(),
		},
	}).Build()

	enrichments := make([]otelattr.KeyValue, len(tests))
	for i, test := range tests {
		enrichments[i] = test.enrichment
	}

	require.NoError(t, applySpanEnrichments(out, enrichments))
	require.Len(t, out.GetAttributes(), len(tests)+1)
	require.Equal(t, "existing", out.GetAttributes()[0].GetKey())
	require.Equal(t, "preserved", out.GetAttributes()[0].GetValue().GetStringValue())

	for i, test := range tests {
		got := out.GetAttributes()[i+1]
		require.Equal(t, test.name, got.GetKey())
		test.check(t, got.GetValue())
	}
}

func TestApplySpanEnrichmentsPreservesEmptyValue(t *testing.T) {
	t.Parallel()

	out := (&otelv1.Span_builder{
		Attributes: []*otelv1.Span_KeyValue{
			(&otelv1.Span_KeyValue_builder{Key: new("existing")}).Build(),
		},
	}).Build()

	err := applySpanEnrichments(out, []otelattr.KeyValue{{
		Key:   "empty",
		Value: otelattr.Value{},
	}})
	require.NoError(t, err)
	require.Len(t, out.GetAttributes(), 2)
	require.Equal(t, "empty", out.GetAttributes()[1].GetKey())
	require.Equal(t, otelv1.Span_AnyValue_Value_not_set_case, out.GetAttributes()[1].GetValue().WhichValue())
}

func TestRewriteInstrumentationScopePreservesOriginalName(t *testing.T) {
	t.Parallel()

	originalName := "com.anthropic.claude_code.tracing"
	version := "1.2.3"
	existingKey := "existing"
	existingValue := "preserved"
	span := (&otelv1.Span_builder{
		Scope: (&otelv1.Span_InstrumentationScope_builder{
			Name:    &originalName,
			Version: &version,
		}).Build(),
		Attributes: []*otelv1.Span_KeyValue{
			(&otelv1.Span_KeyValue_builder{
				Key: &existingKey,
				Value: (&otelv1.Span_AnyValue_builder{
					StringValue: &existingValue,
				}).Build(),
			}).Build(),
		},
	}).Build()

	require.NoError(t, rewriteInstrumentationScope(span))

	require.Equal(t, normalizedInstrumentationScopeName, span.GetScope().GetName())
	require.Equal(t, version, span.GetScope().GetVersion())
	require.Len(t, span.GetAttributes(), 2)
	require.Equal(t, existingKey, span.GetAttributes()[0].GetKey())
	require.Equal(t, existingValue, span.GetAttributes()[0].GetValue().GetStringValue())
	require.Equal(t, string(enrich.OriginalInstrumentationScopeNameKey), span.GetAttributes()[1].GetKey())
	require.Equal(t, originalName, span.GetAttributes()[1].GetValue().GetStringValue())
}

func TestRewriteInstrumentationScopeCreatesMissingScope(t *testing.T) {
	t.Parallel()

	span := (&otelv1.Span_builder{}).Build()

	require.NoError(t, rewriteInstrumentationScope(span))

	require.Equal(t, normalizedInstrumentationScopeName, span.GetScope().GetName())
	require.Empty(t, span.GetAttributes())
}

func TestRewriteInstrumentationScopeLeavesNormalizedScopeUnchanged(t *testing.T) {
	t.Parallel()

	name := normalizedInstrumentationScopeName
	span := (&otelv1.Span_builder{
		Scope: (&otelv1.Span_InstrumentationScope_builder{Name: &name}).Build(),
	}).Build()

	require.NoError(t, rewriteInstrumentationScope(span))

	require.Equal(t, normalizedInstrumentationScopeName, span.GetScope().GetName())
	require.Empty(t, span.GetAttributes())
}

func TestSpanTransformHandlerClassifiesAndPublishes(t *testing.T) {
	t.Parallel()

	inbound := (&otelv1.InboundSpan_builder{
		TraceId:           []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanId:            []byte{1, 2, 3, 4, 5, 6, 7, 8},
		Name:              new("chat gpt-4o"),
		StartTimeUnixNano: new(uint64(1_724_500_000_000_000_001)),
		EndTimeUnixNano:   new(uint64(1_724_500_000_000_000_501)),
		Scope:             (&otelv1.InboundSpan_InstrumentationScope_builder{Name: new("litellm")}).Build(),
		Resource: (&otelv1.InboundSpan_Resource_builder{
			Attributes: []*otelv1.InboundSpan_KeyValue{spanTestStringAttribute("service.name", "LiteLLM")},
		}).Build(),
		Provenance: (&otelv1.InboundSpan_Provenance_builder{
			Source:         new("speakeasy"),
			OrganizationId: new(testLogOrganizationID),
			ProjectId:      new(testLogProjectID),
		}).Build(),
		Attributes: []*otelv1.InboundSpan_KeyValue{
			spanTestStringAttribute("gen_ai.operation.name", "chat"),
			spanTestStringAttribute("gen_ai.provider.name", "openai"),
			spanTestStringAttribute("gen_ai.conversation.id", "session-9"),
		},
	}).Build()

	var published *otelv1.Span
	publisher := gcp.NewMockPublisher[*otelv1.Span]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		span, ok := args.Get(1).(*otelv1.Span)
		require.True(t, ok)
		published = span
	}).Return(gcp.NewSuccessPublishResult()).Once()
	handler := NewSpanTransformHandler(testenv.NewLogger(t), testenv.NewMeterProvider(t), publisher, newTestDatabase(t), cache.NoopCache)

	require.NoError(t, handler.Handle(t.Context(), inbound, gcp.MessageMetadata{}))
	publisher.AssertExpectations(t)
	require.NotNil(t, published)

	attributes := make(map[string]*otelv1.Span_AnyValue, len(published.GetAttributes()))
	for _, item := range published.GetAttributes() {
		attributes[item.GetKey()] = item.GetValue()
	}
	require.Equal(t, normalizedInstrumentationScopeName, published.GetScope().GetName())
	require.Equal(t, "litellm", attributes[string(enrich.OriginalInstrumentationScopeNameKey)].GetStringValue())
	require.Equal(t, testLogOrganizationID, attributes[string(enrich.OrganizationIDKey)].GetStringValue())

	// The column enrichers classified the span on the way through and
	// filled the columns its tables name.
	require.Equal(t, "api_request", attributes[string(enrich.EventTypeColumnKey)].GetStringValue())
	require.Equal(t, "chat gpt-4o", attributes[string(enrich.RawEventNameColumnKey)].GetStringValue())
	require.Equal(t, "litellm", attributes[string(enrich.SourceColumnKey)].GetStringValue())
	require.Equal(t, "openai", attributes[string(enrich.ProviderColumnKey)].GetStringValue())
	require.Equal(t, "session-9", attributes[string(enrich.SessionIDColumnKey)].GetStringValue())
	require.Equal(t, int64(500), attributes[string(enrich.DurationNanoColumnKey)].GetIntValue())
}

func spanTestStringAttribute(key, value string) *otelv1.InboundSpan_KeyValue {
	return (&otelv1.InboundSpan_KeyValue_builder{
		Key:   &key,
		Value: (&otelv1.InboundSpan_AnyValue_builder{StringValue: &value}).Build(),
	}).Build()
}
