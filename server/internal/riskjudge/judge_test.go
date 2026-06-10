package riskjudge

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func TestJudgeRateLimiterPerOrgIsolation(t *testing.T) {
	t.Parallel()

	l := newJudgeRateLimiter()
	now := time.Now()

	// The first burst of calls for one org all pass; the next is throttled.
	for i := range judgeRateBurst {
		require.True(t, l.allow("org-a", now), "call %d for org-a should be allowed", i)
	}
	require.False(t, l.allow("org-a", now), "org-a should be throttled once burst is exhausted")

	// A different org has its own bucket and is unaffected.
	require.True(t, l.allow("org-b", now), "org-b should have an independent bucket")
}

func TestJudgeRateLimitedFailOpenReturnsNil(t *testing.T) {
	t.Parallel()

	client := &countingCompletionClient{}
	j := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), client)
	drainLimiter(j, "org-a")

	verdict := j.Evaluate(t.Context(), ra.JudgeInput{
		OrgID:     "org-a",
		ProjectID: "proj",
		Prompt:    "flag secrets",
		Text:      "some tool call",
		Config:    ra.JudgeConfig{Model: "", Temperature: nil, FailOpen: true},
	})

	require.Nil(t, verdict, "fail-open policy should allow when throttled")
	require.Zero(t, client.calls.Load(), "throttled call must not reach the completion client")
}

func TestJudgeRateLimitedFailClosedReturnsVerdict(t *testing.T) {
	t.Parallel()

	client := &countingCompletionClient{}
	j := New(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), client)
	drainLimiter(j, "org-a")

	verdict := j.Evaluate(t.Context(), ra.JudgeInput{
		OrgID:     "org-a",
		ProjectID: "proj",
		Prompt:    "flag secrets",
		Text:      "some tool call",
		Config:    ra.JudgeConfig{Model: "", Temperature: nil, FailOpen: false},
	})

	require.NotNil(t, verdict, "fail-closed policy should flag when throttled")
	require.Zero(t, client.calls.Load(), "throttled call must not reach the completion client")
}

// drainLimiter exhausts the per-org token bucket so the next Evaluate is
// throttled.
func drainLimiter(j *Judge, org string) {
	now := time.Now()
	for j.limiter.allow(org, now) {
	}
}

// countingCompletionClient records how many times the completion path was hit,
// so tests can assert a throttled Evaluate never reaches the LLM.
type countingCompletionClient struct {
	calls atomic.Int64
}

func (c *countingCompletionClient) GetObjectCompletion(_ context.Context, _ openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	c.calls.Add(1)
	return nil, errors.New("not implemented")
}

func (c *countingCompletionClient) GetCompletion(_ context.Context, _ openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	return nil, errors.New("not implemented")
}

func (c *countingCompletionClient) GetCompletionStream(_ context.Context, _ openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	return nil, errors.New("not implemented")
}

func (c *countingCompletionClient) CreateEmbeddings(_ context.Context, _ string, _ string, _ []string, _ ...openrouter.EmbeddingOption) ([][]float32, error) {
	return nil, errors.New("not implemented")
}
