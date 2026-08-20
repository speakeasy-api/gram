package otel

import (
	"testing"

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

func TestApplySpanEnrichmentsRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	out := (&otelv1.Span_builder{
		Attributes: []*otelv1.Span_KeyValue{
			(&otelv1.Span_KeyValue_builder{Key: new("existing")}).Build(),
		},
	}).Build()

	err := applySpanEnrichments(out, []otelattr.KeyValue{{
		Key:   "invalid",
		Value: otelattr.Value{},
	}})
	require.ErrorContains(t, err, "convert enrichment \"invalid\"")
	require.Len(t, out.GetAttributes(), 1)
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

	rewriteInstrumentationScope(span)

	require.Equal(t, normalizedInstrumentationScopeName, span.GetScope().GetName())
	require.Equal(t, version, span.GetScope().GetVersion())
	require.Len(t, span.GetAttributes(), 2)
	require.Equal(t, existingKey, span.GetAttributes()[0].GetKey())
	require.Equal(t, existingValue, span.GetAttributes()[0].GetValue().GetStringValue())
	require.Equal(t, originalInstrumentationScopeAttr, span.GetAttributes()[1].GetKey())
	require.Equal(t, originalName, span.GetAttributes()[1].GetValue().GetStringValue())
}

func TestRewriteInstrumentationScopeCreatesMissingScope(t *testing.T) {
	t.Parallel()

	span := (&otelv1.Span_builder{}).Build()

	rewriteInstrumentationScope(span)

	require.Equal(t, normalizedInstrumentationScopeName, span.GetScope().GetName())
	require.Empty(t, span.GetAttributes())
}

func TestRewriteInstrumentationScopeLeavesNormalizedScopeUnchanged(t *testing.T) {
	t.Parallel()

	name := normalizedInstrumentationScopeName
	span := (&otelv1.Span_builder{
		Scope: (&otelv1.Span_InstrumentationScope_builder{Name: &name}).Build(),
	}).Build()

	rewriteInstrumentationScope(span)

	require.Equal(t, normalizedInstrumentationScopeName, span.GetScope().GetName())
	require.Empty(t, span.GetAttributes())
}
