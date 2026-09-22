package judgeshadow

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"

	riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/scanners/jev"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/typesafe"
)

type stubEvaluator struct {
	result typesafe.Result
	err    error
	called bool
}

func (s *stubEvaluator) Evaluate(context.Context, json.RawMessage, map[string]typesafe.Question) (typesafe.Result, error) {
	s.called = true
	return s.result, s.err
}

func newHandler(t *testing.T, flags feature.Provider, evaluator typesafe.Evaluator) (*Handler, *stubEvaluator) {
	t.Helper()
	stub, ok := evaluator.(*stubEvaluator)
	require.True(t, ok)
	h, err := NewHandler(slog.Default(), flags, jev.New(evaluator), noop.NewMeterProvider())
	require.NoError(t, err)
	return h, stub
}

func baseEvent() *riskv1.JudgeShadowAnalysis {
	return riskv1.JudgeShadowAnalysis_builder{
		ComparisonId:    new("cmp-1"),
		OrganizationId:  new("org-1"),
		ProjectId:       new("proj-1"),
		Detector:        new(promptpolicy.Source),
		StateJson:       []byte(`{"policy":"no secrets"}`),
		BaselineOutcome: new("clean"),
	}.Build()
}

func TestHandleSkipsWhenDisabled(t *testing.T) {
	t.Parallel()

	flags := &feature.InMemory{}
	stub := &stubEvaluator{}
	h, _ := newHandler(t, flags, stub)

	err := h.Handle(t.Context(), baseEvent(), gcp.MessageMetadata{})

	require.NoError(t, err)
	require.False(t, stub.called, "the judge must not be called for a disabled organization")
}

func TestHandleRejectsInvalidEvent(t *testing.T) {
	t.Parallel()

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagJevPromptPolicyShadow, "org-1", true)

	tests := map[string]*riskv1.JudgeShadowAnalysis{
		"missing comparison id": riskv1.JudgeShadowAnalysis_builder{
			OrganizationId: new("org-1"), Detector: new(promptpolicy.Source),
			StateJson: []byte(`{}`), BaselineOutcome: new("clean"),
		}.Build(),
		"invalid state json": riskv1.JudgeShadowAnalysis_builder{
			ComparisonId: new("cmp-1"), OrganizationId: new("org-1"), Detector: new(promptpolicy.Source),
			StateJson: []byte(`not json`), BaselineOutcome: new("clean"),
		}.Build(),
		"invalid baseline outcome": riskv1.JudgeShadowAnalysis_builder{
			ComparisonId: new("cmp-1"), OrganizationId: new("org-1"), Detector: new(promptpolicy.Source),
			StateJson: []byte(`{}`), BaselineOutcome: new("bogus"),
		}.Build(),
	}

	for name, event := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			stub := &stubEvaluator{}
			h, _ := newHandler(t, flags, stub)

			err := h.Handle(t.Context(), event, gcp.MessageMetadata{})

			require.Error(t, err)
			require.False(t, stub.called)
		})
	}
}

func TestHandleComputesMatchOutcome(t *testing.T) {
	t.Parallel()

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagJevPromptPolicyShadow, "org-1", true)
	stub := &stubEvaluator{result: typesafe.Result{Probabilities: map[string]float64{"policy_match": 0.9}}}
	h, _ := newHandler(t, flags, stub)

	err := h.Handle(t.Context(), baseEvent(), gcp.MessageMetadata{})

	require.NoError(t, err)
	require.True(t, stub.called)
}

func TestHandleSwallowsJudgeError(t *testing.T) {
	t.Parallel()

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagJevPromptPolicyShadow, "org-1", true)
	stub := &stubEvaluator{err: typesafe.ErrUnavailable}
	h, _ := newHandler(t, flags, stub)

	err := h.Handle(t.Context(), baseEvent(), gcp.MessageMetadata{})

	require.NoError(t, err, "a provider failure must not fail (and retry) the shadow message")
	require.True(t, stub.called)
}
