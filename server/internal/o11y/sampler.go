package o11y

import (
	"strings"

	"go.opentelemetry.io/otel/trace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// LLMPrioritySampler always samples LLM-related spans and uses a fallback sampler for others.
type LLMPrioritySampler struct {
	fallback sdktrace.Sampler
}

// NewLLMPrioritySampler creates a sampler that always samples LLM spans.
func NewLLMPrioritySampler(fallback sdktrace.Sampler) sdktrace.Sampler {
	return &LLMPrioritySampler{
		fallback: fallback,
	}
}

func (s *LLMPrioritySampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	// Always sample spans related to LLM operations
	if strings.HasPrefix(p.Name, "llm.") || strings.HasPrefix(p.Name, "gen_ai.") {
		return sdktrace.SamplingResult{
			Decision:   sdktrace.RecordAndSample,
			Tracestate: trace.SpanContextFromContext(p.ParentContext).TraceState(),
		}
	}

	// Use fallback sampler for everything else
	return s.fallback.ShouldSample(p)
}

func (s *LLMPrioritySampler) Description() string {
	return "LLMPrioritySampler{always sample LLM spans, fallback: " + s.fallback.Description() + "}"
}
