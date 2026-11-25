package intentpolicy_test

import (
	"context"
	"maps"

	"github.com/speakeasy-api/gram/server/internal/intentpolicy"
)

type fixedEvaluator struct {
	labels intentpolicy.LabelSet
}

func (f fixedEvaluator) Label(context.Context, intentpolicy.LabelInput) (intentpolicy.LabelSet, intentpolicy.Usage, error) {
	return maps.Clone(f.labels), intentpolicy.Usage{InputTokens: 1, OutputTokens: 1, CostUSD: 0.001, LatencyMs: 1}, nil
}

func (f fixedEvaluator) Descriptor() intentpolicy.Descriptor {
	return intentpolicy.Descriptor{Evaluator: "fixed", Provider: "test", Model: "test", PromptVersion: "1"}
}
