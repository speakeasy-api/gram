package risk_analysis_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/scanners/promptpolicy"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

const testJudgeConcurrency = 8

type cancelingPromptJudge struct {
	calls  atomic.Int64
	cancel context.CancelFunc
	ready  chan struct{}
}

func (j *cancelingPromptJudge) Evaluate(ctx context.Context, _ promptpolicy.Input) (*promptpolicy.Verdict, error) {
	if j.calls.Add(1) == testJudgeConcurrency {
		j.cancel()
		close(j.ready)
	}
	<-j.ready
	return nil, fmt.Errorf("cancel judge: %w", ctx.Err())
}

func TestEvalPromptGuardrailStopsFanoutAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	judge := &cancelingPromptJudge{calls: atomic.Int64{}, cancel: cancel, ready: make(chan struct{})}
	result, err := risk_analysis.EvalPromptGuardrail(
		ctx,
		testenv.NewLogger(t),
		judge.Evaluate,
		mustCELEngine(t),
		"org",
		"project",
		"flag unsafe messages",
		promptpolicy.Config{Temperature: nil, FailOpen: false},
		evalMessages(testJudgeConcurrency*2),
		nil,
		"",
		"",
	)

	require.NoError(t, err)
	require.Equal(t, int64(testJudgeConcurrency), judge.calls.Load())
	require.Len(t, result.Verdicts, testJudgeConcurrency*2)
	for i, verdict := range result.Verdicts {
		require.Equal(t, i, verdict.Index)
		require.True(t, verdict.Matched)
		require.Equal(t, "Policy judge was unavailable; flagged by fail-closed policy.", verdict.Rationale)
	}
}

func TestEvalPromptGuardrailCapsInScopeMessages(t *testing.T) {
	t.Parallel()

	const totalMessages = 205
	judge := &recordingPromptJudge{}
	result, err := risk_analysis.EvalPromptGuardrail(
		t.Context(),
		testenv.NewLogger(t),
		judge.Evaluate,
		mustCELEngine(t),
		"org",
		"project",
		"flag unsafe messages",
		promptpolicy.Config{Temperature: nil, FailOpen: true},
		evalMessages(totalMessages),
		nil,
		"",
		"",
	)

	require.NoError(t, err)
	require.True(t, result.MessageLimitHit)
	require.Equal(t, totalMessages, result.InScopeMessageCount)
	require.Len(t, result.Verdicts, 200)
	require.Len(t, judge.recorded(), 200)
	for i, verdict := range result.Verdicts {
		require.Equal(t, i, verdict.Index)
	}
}

func evalMessages(count int) []risk_analysis.EvalMessage {
	messages := make([]risk_analysis.EvalMessage, count)
	for i := range messages {
		messages[i] = risk_analysis.EvalMessage{
			ID:        uuid.New(),
			Role:      "user",
			Content:   fmt.Sprintf("message %d", i),
			ToolCalls: nil,
		}
	}
	return messages
}
