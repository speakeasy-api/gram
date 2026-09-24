package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/judgemessage"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/typesafe"
)

type policyPrefilterMock struct{ mock.Mock }

func (m *policyPrefilterMock) Evaluate(ctx context.Context, orgID string, state json.RawMessage, questions map[string]typesafe.Question) (typesafe.Result, error) {
	args := m.Called(ctx, orgID, state, questions)
	result, ok := args.Get(0).(typesafe.Result)
	if !ok {
		return typesafe.Result{Probabilities: nil, Model: "", InputTokens: 0, OutputTokens: 0, CostUSD: 0}, errors.New("invalid test result")
	}
	return result, args.Error(1)
}

type policyWindowMock struct{ mock.Mock }

func (m *policyWindowMock) Load(ctx context.Context, orgID, projectID string, msg judgemessage.Message) (judgemessage.Window, error) {
	args := m.Called(ctx, orgID, projectID, msg)
	result, ok := args.Get(0).(judgemessage.Window)
	if !ok {
		return judgemessage.Window{Messages: nil, TargetIndex: 0}, errors.New("invalid test window")
	}
	return result, args.Error(1)
}

func TestCascadeThresholdAndIndependentConfirmation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		probability float64
		matched     bool
		escalates   bool
	}{
		{name: "below", probability: 0.89999, matched: false, escalates: false},
		{name: "boundary confirmed", probability: 0.9, matched: true, escalates: true},
		{name: "false positive dismissed", probability: 0.99, matched: false, escalates: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := judgemessage.New(message.ToolRequest, "Bash", `{"command":"rm example.txt"}`)
			input := promptpolicy.Input{OrgID: "org-example", ProjectID: "project-example", UserID: "", Prompt: "Block deleting files unless the user explicitly requested it.", Message: msg, Config: promptpolicy.Config{Temperature: nil, FailOpen: false}}
			rawVerdict := `{"matched":false,"confidence":0.95,"rationale":"Message 0 authorizes the target in message 2."}`
			if tc.matched {
				rawVerdict = `{"matched":true,"confidence":0.95,"rationale":"The target in message 2 deletes a file without authorization."}`
			}
			completion := &successfulCompletionClient{body: rawVerdict}
			prefilter := &policyPrefilterMock{}
			windows := &policyWindowMock{}
			prefilter.On("Evaluate", mock.Anything, input.OrgID, mock.MatchedBy(func(raw json.RawMessage) bool {
				var payload map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(raw, &payload))
				require.Contains(t, string(payload["policy"]), "explicitly requested")
				require.Contains(t, string(payload["message"]), "ai_assistant_tool_call")
				return true
			}), mock.MatchedBy(func(q map[string]typesafe.Question) bool { return q["policy_match"].Type == "noul" })).Return(typesafe.Result{Probabilities: map[string]float64{"policy_match": tc.probability}, Model: typesafe.Model, InputTokens: 20, OutputTokens: 1, CostUSD: 0.001}, nil).Once()
			window := judgemessage.Window{Messages: []judgemessage.Payload{
				judgemessage.RenderPayload(judgemessage.New(message.User, "", "Delete the example file.")),
				judgemessage.RenderPayload(judgemessage.New(message.Assistant, "", "I will remove it.")),
				judgemessage.RenderPayload(msg),
				judgemessage.RenderPayload(judgemessage.New(message.ToolResponse, "Bash", "removed")),
				judgemessage.RenderPayload(judgemessage.New(message.Assistant, "", "Done.")),
			}, TargetIndex: 2}
			if tc.escalates {
				windows.On("Load", mock.Anything, input.OrgID, input.ProjectID, msg).Return(window, nil).Once()
			}
			cascade := &Cascade{judge: newTestJudge(t, completion), prefilter: prefilter, windows: windows, enabled: func(context.Context, string, string) (bool, error) { return true, nil }, slots: make(chan struct{}, 1)}
			verdict, err := cascade.Evaluate(t.Context(), input)
			require.NoError(t, err)
			require.NotNil(t, verdict)
			require.True(t, verdict.Completed)
			require.Equal(t, tc.matched, verdict.Matched)
			require.InDelta(t, 0.001, verdict.CostUSD, 0.000001)
			require.Positive(t, verdict.STokens)
			if tc.escalates {
				require.Equal(t, CascadeModel, completion.lastReq.Model)
				require.Equal(t, ContextSystemPrompt, completion.lastReq.SystemPrompt)
				var payload struct {
					Conversation judgemessage.Window `json:"conversation"`
				}
				require.NoError(t, json.Unmarshal([]byte(completion.lastReq.Prompt), &payload))
				require.Len(t, payload.Conversation.Messages, 5)
				require.Equal(t, 2, payload.Conversation.TargetIndex)
				require.Equal(t, 1, strings.Count(completion.lastReq.Prompt, "rm example.txt"), "the target appears once in the five-message window")
				require.NotContains(t, completion.lastReq.Prompt, "probability")
				require.NotContains(t, completion.lastReq.Prompt, "0.99")
			} else {
				require.Empty(t, completion.lastReq.Model)
				require.Equal(t, typesafe.Model, verdict.Model)
			}
			prefilter.AssertExpectations(t)
			windows.AssertExpectations(t)
		})
	}
}

func TestCascadeFailuresNeverBecomePolicyFindings(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"prefilter", "missing probability", "invalid probability", "context", "opus", "malformed opus"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			msg := judgemessage.New(message.User, "", "Delete the example file.")
			input := promptpolicy.Input{OrgID: "org-example", ProjectID: "project-example", UserID: "", Prompt: "Block deletion.", Message: msg, Config: promptpolicy.Config{Temperature: nil, FailOpen: false}}
			result := typesafe.Result{Probabilities: map[string]float64{"policy_match": 0.99}, Model: typesafe.Model, InputTokens: 1, OutputTokens: 1, CostUSD: 0}
			var prefilterErr error
			switch name {
			case "prefilter":
				prefilterErr = errors.New("provider unavailable")
			case "missing probability":
				result.Probabilities = nil
			case "invalid probability":
				result.Probabilities["policy_match"] = math.NaN()
			}
			prefilter := &policyPrefilterMock{}
			prefilter.On("Evaluate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(result, prefilterErr).Once()
			windows := &policyWindowMock{}
			if name == "context" || name == "opus" || name == "malformed opus" {
				var windowErr error
				if name == "context" {
					windowErr = errors.New("anchor unavailable")
				}
				windows.On("Load", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(judgemessage.Window{Messages: []judgemessage.Payload{judgemessage.RenderPayload(msg)}, TargetIndex: 0}, windowErr).Once()
			}
			judge := newTestJudge(t, &countingCompletionClient{err: errors.New("review unavailable")})
			if name == "malformed opus" {
				judge = newTestJudge(t, &successfulCompletionClient{body: `{"matched":true}`})
			}
			cascade := &Cascade{judge: judge, prefilter: prefilter, windows: windows, enabled: func(context.Context, string, string) (bool, error) { return true, nil }, slots: make(chan struct{}, 1)}
			verdict, err := cascade.Evaluate(t.Context(), input)
			require.ErrorIs(t, err, promptpolicy.ErrNoVerdict)
			require.Nil(t, verdict)
			require.Empty(t, promptpolicy.FindingsFromEvaluation(input.Config, verdict, err, false))
			prefilter.AssertExpectations(t)
			windows.AssertExpectations(t)
		})
	}
}

func TestCascadeDisabledOrUnavailableKeepsBaseline(t *testing.T) {
	t.Parallel()
	for _, gateErr := range []error{nil, errors.New("flag lookup failed")} {
		completion := &successfulCompletionClient{body: `{"matched":true,"confidence":0.95,"rationale":"Policy violation."}`}
		prefilter := &policyPrefilterMock{}
		windows := &policyWindowMock{}
		cascade := &Cascade{judge: newTestJudge(t, completion), prefilter: prefilter, windows: windows, enabled: func(context.Context, string, string) (bool, error) { return false, gateErr }, slots: make(chan struct{}, 1)}
		verdict, err := cascade.Evaluate(t.Context(), promptpolicy.Input{OrgID: "org-example", ProjectID: "project-example", UserID: "", Prompt: "Block deletion.", Message: judgemessage.New(message.User, "", "Delete the file."), Config: promptpolicy.Config{Temperature: nil, FailOpen: false}})
		require.NoError(t, err)
		require.True(t, verdict.Matched)
		require.Equal(t, judgeModel, completion.lastReq.Model)
		prefilter.AssertNotCalled(t, "Evaluate", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		windows.AssertNotCalled(t, "Load", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	}
}
