package otel

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestNewEnrichSpeakeasyTokens(t *testing.T) {
	t.Parallel()

	enricher := NewEnrichSpeakeasyTokens()

	require.Equal(t, "enrich-speakeasy-tokens", enricher.Name())
	require.NotNil(t, enricher.codec)
	require.Equal(t, "o200k_base", enricher.codec.GetName())
}

func TestEnrichSpeakeasyTokensCountsClaudeCodePrompt(t *testing.T) {
	t.Parallel()

	prompt := "The quick brown fox jumps over the lazy dog"
	scopeName := "com.anthropic.claude_code.tracing"
	attributeKey := "user_prompt"
	span := (&otelv1.InboundSpan_builder{
		Scope: (&otelv1.InboundSpan_InstrumentationScope_builder{
			Name: &scopeName,
		}).Build(),
		Attributes: []*otelv1.InboundSpan_KeyValue{
			(&otelv1.InboundSpan_KeyValue_builder{
				Key: &attributeKey,
				Value: (&otelv1.InboundSpan_AnyValue_builder{
					StringValue: &prompt,
				}).Build(),
			}).Build(),
		},
	}).Build()

	got, err := NewEnrichSpeakeasyTokens().Enrich(t.Context(), span)

	require.NoError(t, err)
	require.Equal(t, []attribute.KeyValue{
		attribute.Int("speakeasy.tokens.count", 9),
		attribute.String("speakeasy.tokens.codec", "o200k_base"),
	}, got)
}

func TestEnrichSpeakeasyTokensSkipsEmptyClaudeCodePrompt(t *testing.T) {
	t.Parallel()

	prompt := ""
	scopeName := "com.anthropic.claude_code.tracing"
	attributeKey := "user_prompt"
	span := (&otelv1.InboundSpan_builder{
		Scope: (&otelv1.InboundSpan_InstrumentationScope_builder{
			Name: &scopeName,
		}).Build(),
		Attributes: []*otelv1.InboundSpan_KeyValue{
			(&otelv1.InboundSpan_KeyValue_builder{
				Key: &attributeKey,
				Value: (&otelv1.InboundSpan_AnyValue_builder{
					StringValue: &prompt,
				}).Build(),
			}).Build(),
		},
	}).Build()

	got, err := NewEnrichSpeakeasyTokens().Enrich(t.Context(), span)

	require.NoError(t, err)
	require.Nil(t, got)
}
