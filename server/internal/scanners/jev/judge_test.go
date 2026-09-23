package jev

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/scanners/promptinjection"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/typesafe"
)

type stubEvaluator struct {
	result typesafe.Result
	err    error
	got    map[string]typesafe.Question
	orgID  string
	state  json.RawMessage
}

func (s *stubEvaluator) Evaluate(_ context.Context, orgID string, state json.RawMessage, questions map[string]typesafe.Question) (typesafe.Result, error) {
	s.got = questions
	s.orgID = orgID
	s.state = state
	return s.result, s.err
}

func TestQuestionsPromptPolicy(t *testing.T) {
	t.Parallel()

	questions, err := Questions(promptpolicy.Source)

	require.NoError(t, err)
	require.Contains(t, questions, "policy_match")
	require.Equal(t, "noul", questions["policy_match"].Type)
}

func TestQuestionsPromptInjection(t *testing.T) {
	t.Parallel()

	questions, err := Questions(promptinjection.Source)

	require.NoError(t, err)
	require.Len(t, questions, 3)
	for id, q := range questions {
		require.Equal(t, "noul", q.Type, "question %q", id)
	}
}

func TestQuestionsUnsupportedDetector(t *testing.T) {
	t.Parallel()

	_, err := Questions("some_other_detector")

	require.ErrorContains(t, err, "unsupported Jev detector")
}

func TestJudgeEvaluateUnsupportedDetectorSkipsClient(t *testing.T) {
	t.Parallel()

	evaluator := &stubEvaluator{}
	judge := New(evaluator)

	_, err := judge.Evaluate(t.Context(), "org-1", "some_other_detector", json.RawMessage(`{}`))

	require.Error(t, err)
	require.Nil(t, evaluator.got, "client must not be called for an unsupported detector")
}

func TestJudgeEvaluateDelegatesToClient(t *testing.T) {
	t.Parallel()

	evaluator := &stubEvaluator{result: typesafe.Result{Probabilities: map[string]float64{"policy_match": 0.9}, Model: typesafe.Model, InputTokens: 0, OutputTokens: 0, CostUSD: 0}}
	judge := New(evaluator)

	result, err := judge.Evaluate(t.Context(), "org-1", promptpolicy.Source, json.RawMessage(`{}`))

	require.NoError(t, err)
	require.InDelta(t, 0.9, result.Probabilities["policy_match"], 1e-9)
	require.Equal(t, "org-1", evaluator.orgID)
	require.JSONEq(t, `{}`, string(evaluator.state))
	require.Contains(t, evaluator.got, "policy_match")
}
