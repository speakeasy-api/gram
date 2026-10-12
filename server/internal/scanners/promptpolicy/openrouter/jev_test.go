package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	typesafe "github.com/speakeasy-api/gram/server/internal/thirdparty/typesafedecisions"
)

type policyEvaluatorMock struct{ mock.Mock }

func (m *policyEvaluatorMock) Evaluate(ctx context.Context, orgID string, state json.RawMessage, questions map[string]typesafe.Question) (typesafe.Result, error) {
	args := m.Called(ctx, orgID, state, questions)
	result, ok := args.Get(0).(typesafe.Result)
	if !ok {
		return typesafe.Result{Probabilities: nil, Model: "", InputTokens: 0, OutputTokens: 0, CostUSD: 0}, errors.New("invalid test result")
	}
	return result, args.Error(1)
}

func TestJevPolicyThresholdWithoutSecondaryCalls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		probability float64
		matched     bool
	}{
		{name: "negative", probability: 0, matched: false},
		{name: "below", probability: 0.89999, matched: false},
		{name: "boundary", probability: 0.9, matched: true},
		{name: "high", probability: 0.99, matched: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := promptpolicy.Input{OrgID: "org-example", ProjectID: "project-example", UserID: "", Prompt: "Block deleting files unless the user explicitly requested it.", Message: judgemessage.New(message.ToolRequest, "Bash", `{"command":"rm example.txt"}`), Config: promptpolicy.Config{Temperature: nil, FailOpen: false}}
			completion := &countingCompletionClient{}
			evaluator := &policyEvaluatorMock{}
			evaluator.On("Evaluate", mock.Anything, input.OrgID, mock.MatchedBy(func(raw json.RawMessage) bool {
				var payload map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(raw, &payload))
				require.Contains(t, string(payload["policy"]), "explicitly requested")
				require.Contains(t, string(payload["message"]), "ai_assistant_tool_call")
				require.NotContains(t, payload, "conversation")
				return true
			}), mock.MatchedBy(func(q map[string]typesafe.Question) bool { return q["policy_match"].Type == "noul" })).Return(typesafe.Result{Probabilities: map[string]float64{"policy_match": tc.probability}, Model: typesafe.Model, InputTokens: 20, OutputTokens: 1, CostUSD: 0.001}, nil).Once()
			judge := &JevJudge{judge: newTestJudge(t, completion), evaluator: evaluator, enabled: func(context.Context, string, string) (bool, error) { return true, nil }, slots: make(chan struct{}, 1)}
			verdict, err := judge.Evaluate(t.Context(), input)
			require.NoError(t, err)
			require.NotNil(t, verdict)
			require.True(t, verdict.Completed)
			require.Equal(t, tc.matched, verdict.Matched)
			require.InDelta(t, tc.probability, verdict.Confidence, 0.000001)
			require.InDelta(t, 0.001, verdict.CostUSD, 0.000001)
			require.Equal(t, 21, verdict.TotalTokens)
			require.Positive(t, verdict.STokens)
			require.Equal(t, typesafe.Model, verdict.Model)
			require.Zero(t, completion.calls.Load(), "enabled Jev must never call a secondary completion model")
			findings := promptpolicy.FindingsFromEvaluation(input.Config, verdict, nil, false)
			if tc.matched {
				require.Len(t, findings, 1)
				require.NotEmpty(t, findings[0].Description)
			} else {
				require.Empty(t, findings)
			}
			evaluator.AssertExpectations(t)
		})
	}
}

func TestJevPolicyFailuresNeverBecomeFindings(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"provider", "missing probability", "nan", "infinite", "negative", "above one"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := typesafe.Result{Probabilities: map[string]float64{"policy_match": 0.99}, Model: typesafe.Model, InputTokens: 1, OutputTokens: 1, CostUSD: 0}
			var providerErr error
			switch name {
			case "provider":
				providerErr = errors.New("provider unavailable")
			case "missing probability":
				result.Probabilities = nil
			case "nan":
				result.Probabilities["policy_match"] = math.NaN()
			case "infinite":
				result.Probabilities["policy_match"] = math.Inf(1)
			case "negative":
				result.Probabilities["policy_match"] = -0.1
			case "above one":
				result.Probabilities["policy_match"] = 1.1
			}
			evaluator := &policyEvaluatorMock{}
			evaluator.On("Evaluate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(result, providerErr).Once()
			completion := &countingCompletionClient{}
			judge := &JevJudge{judge: newTestJudge(t, completion), evaluator: evaluator, enabled: func(context.Context, string, string) (bool, error) { return true, nil }, slots: make(chan struct{}, 1)}
			input := promptpolicy.Input{OrgID: "org-example", ProjectID: "project-example", UserID: "", Prompt: "Block deletion.", Message: judgemessage.New(message.User, "", "Delete the example file."), Config: promptpolicy.Config{Temperature: nil, FailOpen: false}}
			verdict, err := judge.Evaluate(t.Context(), input)
			require.ErrorIs(t, err, promptpolicy.ErrNoVerdict)
			require.Nil(t, verdict)
			require.Empty(t, promptpolicy.FindingsFromEvaluation(input.Config, verdict, err, false))
			require.Zero(t, completion.calls.Load())
			evaluator.AssertExpectations(t)
		})
	}
}

func TestJevPolicyDisabledOrUnavailableKeepsBaseline(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		gateErr error
	}{{name: "disabled", gateErr: nil}, {name: "unavailable", gateErr: errors.New("flag lookup failed")}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			completion := &successfulCompletionClient{body: `{"matched":true,"confidence":0.95,"rationale":"Policy violation."}`}
			evaluator := &policyEvaluatorMock{}
			judge := &JevJudge{judge: newTestJudge(t, completion), evaluator: evaluator, enabled: func(context.Context, string, string) (bool, error) { return false, tc.gateErr }, slots: make(chan struct{}, 1)}
			verdict, err := judge.Evaluate(t.Context(), promptpolicy.Input{OrgID: "org-example", ProjectID: "project-example", UserID: "", Prompt: "Block deletion.", Message: judgemessage.New(message.User, "", "Delete the file."), Config: promptpolicy.Config{Temperature: nil, FailOpen: false}})
			require.NoError(t, err)
			require.True(t, verdict.Matched)
			require.Equal(t, judgeModel, completion.lastReq.Model)
			evaluator.AssertNotCalled(t, "Evaluate", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		})
	}
}
