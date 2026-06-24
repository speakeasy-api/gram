package riskjudge

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/stretchr/testify/require"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// observeFakeClient returns a configurable verdict (or error) plus a usage
// payload, so the test can assert JudgeInput.Observe fires on every code path.
type observeFakeClient struct {
	calls   atomic.Int64
	verdict string
	err     error
	cost    *float64
}

func (c *observeFakeClient) GetObjectCompletion(_ context.Context, _ openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	c.calls.Add(1)
	if c.err != nil {
		return nil, c.err
	}
	content := or.CreateChatAssistantMessageContentStr(c.verdict)
	msg := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{
		Role:    or.ChatAssistantMessageRoleAssistant,
		Content: optionalnullable.From(&content),
	})
	return &openrouter.CompletionResponse{
		Message: &msg,
		Usage:   openrouter.Usage{PromptTokens: 42, CompletionTokens: 7, Cost: c.cost},
	}, nil
}

func (c *observeFakeClient) GetCompletion(_ context.Context, _ openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	return nil, errors.New("not implemented")
}

func (c *observeFakeClient) GetCompletionStream(_ context.Context, _ openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	return nil, errors.New("not implemented")
}

func (c *observeFakeClient) CreateEmbeddings(_ context.Context, _ string, _ string, _ []string, _ ...openrouter.EmbeddingOption) ([][]float32, error) {
	return nil, errors.New("not implemented")
}

func TestJudgeObserveFiresOnEveryCall(t *testing.T) {
	t.Parallel()

	cost := 0.0125
	cases := []struct {
		name     string
		verdict  string
		err      error
		wantErr  bool
		wantCost bool
	}{
		{name: "match", verdict: `{"matched":true,"confidence":0.9,"rationale":"flagged"}`, wantCost: true},
		{name: "no match", verdict: `{"matched":false,"confidence":0.1,"rationale":"clean"}`, wantCost: true},
		{name: "judge error", err: errors.New("openrouter exploded"), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := &observeFakeClient{verdict: tc.verdict, err: tc.err, cost: &cost}
			j := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), client)

			var observed []ra.JudgeUsage
			j.Evaluate(t.Context(), ra.JudgeInput{
				OrgID:     "org-a",
				ProjectID: "proj",
				Prompt:    "flag destructive commands",
				Message:   ra.NewJudgeMessage(message.ToolRequest, "Bash", `{"command":"rm -rf /"}`),
				Config:    ra.JudgeConfig{Model: "", Temperature: nil, FailOpen: false},
				Observe:   func(u ra.JudgeUsage) { observed = append(observed, u) },
			})

			require.Equal(t, int64(1), client.calls.Load(), "the model call should be attempted exactly once")
			require.Len(t, observed, 1, "Observe must fire exactly once per attempted call regardless of verdict")

			if tc.wantErr {
				require.Error(t, observed[0].Err)
			} else {
				require.NoError(t, observed[0].Err)
			}
			if tc.wantCost {
				require.Equal(t, 42, observed[0].InputTokens)
				require.Equal(t, 7, observed[0].OutputTokens)
				require.NotNil(t, observed[0].CostUSD)
			}
		})
	}
}

func TestJudgeObserveNilIsNoop(t *testing.T) {
	t.Parallel()

	// With no Observe sink the realtime path must still work and not panic.
	client := &observeFakeClient{verdict: `{"matched":false,"confidence":0,"rationale":"ok"}`}
	j := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), client)

	require.NotPanics(t, func() {
		j.Evaluate(t.Context(), ra.JudgeInput{
			OrgID:     "org-a",
			ProjectID: "proj",
			Prompt:    "flag things",
			Message:   ra.NewJudgeMessage(message.ToolRequest, "Bash", `{"command":"ls"}`),
			Config:    ra.JudgeConfig{Model: "", Temperature: nil, FailOpen: false},
			Observe:   nil,
		})
	})
	require.Equal(t, int64(1), client.calls.Load())
}
