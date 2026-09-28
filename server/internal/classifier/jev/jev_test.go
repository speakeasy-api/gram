package jev

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Classifier {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	return New(policy, conv.NewSecret([]byte("test-key")), WithEndpoint(server.URL))
}

type mockLimiter struct{ mock.Mock }

func (m *mockLimiter) AllowN(ctx context.Context, key guardian.Partition, limit guardian.Limit, n uint32) (guardian.RateLimitResult, error) {
	args := m.Called(ctx, key, limit, n)
	result, ok := args.Get(0).(guardian.RateLimitResult)
	if !ok {
		panic("mockLimiter: expected guardian.RateLimitResult")
	}
	return result, args.Error(1)
}

func TestGuardianLimitsEverySplitAttempt(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(413)
	}))
	t.Cleanup(server.Close)
	var limiter mockLimiter
	limiter.Test(t)
	t.Cleanup(func() { limiter.AssertExpectations(t) })
	limit := guardian.Limit{Rate: 1200, Burst: 20, Period: time.Minute}
	var admitted guardian.RateLimitResult
	admitted.Limit = limit
	admitted.Allowed = 1
	limiter.On("AllowN", mock.Anything, mock.Anything, limit, uint32(1)).Return(admitted, nil).Times(3)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil, guardian.WithLimiter(&limiter))
	require.NoError(t, err)
	c := New(policy, conv.NewSecret([]byte("test-key")), WithEndpoint(server.URL))
	req := classifier.NewRequest(classifier.Text("state")).Ask(classifier.Noul("a", classifier.Text("a"))).Ask(classifier.Noul("b", classifier.Text("b")))
	result := c.Classify(t.Context(), req)
	require.ErrorIs(t, result.Err(), classifier.ErrRequestTooLarge)
	require.Equal(t, int32(3), calls.Load())
}

func TestClassifyAllVariants(t *testing.T) {
	t.Parallel()
	var captured wireRequest
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Method != http.MethodPost {
			w.WriteHeader(401)
			return
		}
		if json.NewDecoder(r.Body).Decode(&captured) != nil {
			w.WriteHeader(400)
			return
		}
		_, _ = fmt.Fprint(w, `{"model":"jev-1.13.0","answers":{"0":{"type":"noul","noul":0.8},"1":{"type":"choice","choice":"1","probabilities":{"0":0.1,"1":0.9},"confidence":0.7},"2":{"type":"score","score":0.75,"probabilities":{"0":0.25,"1":0.75},"confidence":0.6}},"usage":{"input_tokens":100,"output_tokens":10}}`)
	})
	state, err := classifier.ParseEntry([]byte(`{"messages":["hello"]}`))
	require.NoError(t, err)
	instructions, err := classifier.ParseEntry([]byte(`{"task":"classify","nested":[true,12]}`))
	require.NoError(t, err)
	req := classifier.NewRequest(state).
		Ask(classifier.Noul("noul", instructions)).
		Ask(classifier.Choice("choice", instructions, classifier.NewOption("private-a", instructions), classifier.NewOption("private-b", classifier.Text("b")))).
		Ask(classifier.Score("score", instructions, classifier.NewOption("low", classifier.Text("low")), classifier.NewOption("high", classifier.Text("high"))))
	result := c.Classify(t.Context(), req)
	require.NoError(t, result.Err())
	require.Len(t, result.Outcomes, 3)
	for _, outcome := range result.Outcomes {
		require.Nil(t, outcome.Failure)
		require.NotNil(t, outcome.Answer)
	}
	require.InDelta(t, 0.8, result.Outcomes[0].Answer.Noul.Probability, 1e-9)
	require.Equal(t, classifier.OptionKey("private-b"), result.Outcomes[1].Answer.Choice.Selected)
	require.Equal(t, classifier.OptionKey("low"), result.Outcomes[2].Answer.Score.Distribution[0].Option)
	require.InDelta(t, 0.75, result.Outcomes[2].Answer.Score.ExpectedIndex, 1e-9)
	require.True(t, result.Usage.Complete)
	require.Equal(t, int64(100), result.Usage.InputTokens)
	require.Equal(t, model, captured.Model)
	encoded, err := json.Marshal(captured)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-a")
	require.Contains(t, string(encoded), `"nested":[true,12]`)
	require.Equal(t, state, captured.State)
}

func TestClassifySizeRejectionSplitsAndKeepsPartialSuccess(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req wireRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(400)
			return
		}
		if len(req.Questions) > 1 {
			w.WriteHeader(413)
			return
		}
		if _, exists := req.Questions["1"]; exists {
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(429)
			return
		}
		_, _ = fmt.Fprint(w, `{"model":"jev-1.13.0","answers":{"0":{"type":"noul","noul":0.4}},"usage":{"input_tokens":20,"output_tokens":2}}`)
	})
	req := classifier.NewRequest(classifier.Text("state")).Ask(classifier.Noul("a", classifier.Text("a"))).Ask(classifier.Noul("b", classifier.Text("b")))
	result := c.Classify(t.Context(), req)
	require.NoError(t, result.Err())
	require.Equal(t, int32(3), calls.Load())
	require.Len(t, result.Outcomes, 2)
	require.NotNil(t, result.Outcomes[0].Answer)
	require.Equal(t, classifier.FailureRateLimited, result.Outcomes[1].Failure.Code)
	require.True(t, result.Outcomes[1].Failure.Retryable)
	require.NotNil(t, result.Outcomes[1].Failure.RetryAfter)
	require.InDelta(t, 5.0, result.Outcomes[1].Failure.RetryAfter.Seconds(), 1e-9)
	require.False(t, result.Usage.Complete)
	require.Equal(t, int64(20), result.Usage.InputTokens)
}

func TestClassifySendsLargeBatchWhole(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	var captured wireRequest
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if json.NewDecoder(r.Body).Decode(&captured) != nil {
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(422)
	})
	req := classifier.NewRequest(classifier.Text(strings.Repeat("s", 100000)))
	for i := range 5 {
		req.Ask(classifier.Noul(classifier.QuestionKey(fmt.Sprint(i)), classifier.Text(strings.Repeat("x", 20000))))
	}
	result := c.Classify(t.Context(), req)
	require.NoError(t, result.Err())
	require.Equal(t, int32(1), calls.Load())
	require.Len(t, captured.Questions, 5)
	require.Equal(t, req.Input, captured.State)
	for _, outcome := range result.Outcomes {
		require.Equal(t, classifier.FailureProviderRejected, outcome.Failure.Code)
	}
}

func TestClassifyUnsplittableSizeErrorPreservesSuccess(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req wireRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(400)
			return
		}
		if _, exists := req.Questions["0"]; exists {
			w.WriteHeader(413)
			return
		}
		_, _ = fmt.Fprint(w, `{"model":"jev-1.13.0","answers":{"1":{"type":"noul","noul":0.4}},"usage":{"input_tokens":20,"output_tokens":2}}`)
	})
	req := classifier.NewRequest(classifier.Text("state")).Ask(classifier.Noul("large", classifier.Text("a"))).Ask(classifier.Noul("fits", classifier.Text("b")))
	result := c.Classify(t.Context(), req)
	require.ErrorIs(t, result.Err(), classifier.ErrRequestTooLarge)
	require.Equal(t, int32(3), calls.Load())
	require.Len(t, result.Outcomes, 2)
	require.Equal(t, classifier.FailureInputTooLarge, result.Outcomes[0].Failure.Code)
	require.False(t, result.Outcomes[0].Failure.Retryable)
	require.NotNil(t, result.Outcomes[1].Answer)
	require.Equal(t, int64(20), result.Usage.InputTokens)
	require.False(t, result.Usage.Complete)
}

func TestClassifySingleQuestion413Stops(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(413) })
	result := c.Classify(t.Context(), classifier.NewRequest(classifier.Text("state")).Ask(classifier.Noul("q", classifier.Text("question"))))
	require.ErrorIs(t, result.Err(), classifier.ErrRequestTooLarge)
	require.Equal(t, int32(1), calls.Load())
	require.Len(t, result.Outcomes, 1)
	require.Equal(t, classifier.FailureInputTooLarge, result.Outcomes[0].Failure.Code)
}

func TestClassifyRepeatedSizeRejectionsTerminate(t *testing.T) {
	t.Parallel()
	const questionCount = 7 // Odd batches exercise uneven splits.
	var calls, singletons atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		attempt := calls.Add(1)
		var req wireRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(400)
			return
		}
		if len(req.Questions) == 1 {
			singletons.Add(1)
		}
		// Both supported size errors must make the same strictly decreasing progress.
		if attempt%2 == 0 {
			w.WriteHeader(400)
			_, _ = fmt.Fprint(w, `{"detail":{"error_type":"max_tokens_exceeded"}}`)
		} else {
			w.WriteHeader(413)
		}
	})
	req := classifier.NewRequest(classifier.Text("state"))
	for i := range questionCount {
		req.Ask(classifier.Noul(classifier.QuestionKey(fmt.Sprint(i)), classifier.Text("question")))
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result := c.Classify(ctx, req)
	require.ErrorIs(t, result.Err(), classifier.ErrRequestTooLarge)
	require.NotErrorIs(t, result.Err(), context.DeadlineExceeded)
	require.Equal(t, int32(2*questionCount-1), calls.Load())
	require.Equal(t, int32(questionCount), singletons.Load())
	require.Len(t, result.Outcomes, questionCount)
	for i, outcome := range result.Outcomes {
		require.Equal(t, req.Questions[i].Key, outcome.Key)
		require.Nil(t, outcome.Answer)
		require.Equal(t, classifier.FailureInputTooLarge, outcome.Failure.Code)
	}
}

func TestClassifyContextOverflowSplitsAndPreservesSuccess(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req wireRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(400)
			return
		}
		if _, exists := req.Questions["0"]; exists {
			w.WriteHeader(400)
			_, _ = fmt.Fprint(w, `{"detail":{"error_type":"max_tokens_exceeded"}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"model":"jev-1.13.0","answers":{"1":{"type":"noul","noul":0.4}},"usage":{"input_tokens":20,"output_tokens":2}}`)
	})
	req := classifier.NewRequest(classifier.Text("state")).Ask(classifier.Noul("large", classifier.Text("a"))).Ask(classifier.Noul("fits", classifier.Text("b")))
	result := c.Classify(t.Context(), req)
	require.ErrorIs(t, result.Err(), classifier.ErrRequestTooLarge)
	require.Equal(t, int32(3), calls.Load())
	require.Len(t, result.Outcomes, 2)
	require.Equal(t, classifier.FailureInputTooLarge, result.Outcomes[0].Failure.Code)
	require.NotNil(t, result.Outcomes[1].Answer)
	require.Equal(t, int64(20), result.Usage.InputTokens)
	require.False(t, result.Usage.Complete)
}

func TestClassifyOtherBadRequestsDoNotSplit(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"detail":{"error_type":"invalid_question"}}`,
		`{"detail":"max_tokens_exceeded"}`,
		`{"error_type":"max_tokens_exceeded"}`,
		`{"detail":{"error_type":"max_tokens_exceeded"}} trailing`,
		`{"detail":`,
	} {
		var calls atomic.Int32
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(400)
			_, _ = fmt.Fprint(w, body)
		})
		req := classifier.NewRequest(classifier.Text("state")).Ask(classifier.Noul("a", classifier.Text("a"))).Ask(classifier.Noul("b", classifier.Text("b")))
		result := c.Classify(t.Context(), req)
		require.NoError(t, result.Err(), body)
		require.Equal(t, int32(1), calls.Load(), body)
		require.Len(t, result.Outcomes, 2)
		for outcome := range result.Iter() {
			require.Equal(t, classifier.FailureProviderRejected, outcome.Failure.Code, body)
		}
	}
}

func TestClassifyValidatesEntireBatchBeforeSending(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) })
	req := classifier.NewRequest(classifier.Text("state")).Ask(classifier.Noul("same", classifier.Text("first"))).Ask(classifier.Noul("same", classifier.Text("second")))
	result := c.Classify(t.Context(), req)
	require.Error(t, result.Err())
	require.Zero(t, calls.Load())
	result = c.Classify(t.Context(), nil)
	require.Error(t, result.Err())
}

func TestClassifyInvalidAnswerDoesNotDiscardOtherAnswers(t *testing.T) {
	t.Parallel()
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"model":"jev-1.13.0","answers":{"0":{"type":"noul","noul":0.4},"1":{"type":"noul","noul":null}}}`)
	})
	req := classifier.NewRequest(classifier.Text("state")).Ask(classifier.Noul("a", classifier.Text("a"))).Ask(classifier.Noul("b", classifier.Text("b")))
	result := c.Classify(t.Context(), req)
	require.NoError(t, result.Err())
	require.NotNil(t, result.Outcomes[0].Answer)
	require.Equal(t, classifier.FailureInvalidResponse, result.Outcomes[1].Failure.Code)
	require.False(t, result.Usage.Complete)
}

func TestClassifyCancellationPreservesUnknownUsage(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	release := make(chan struct{})
	defer close(release)
	c := testClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		cancel()
		<-release
	})
	req := classifier.NewRequest(classifier.Text("state")).Ask(classifier.Noul("a", classifier.Text("a")))
	result := c.Classify(ctx, req)
	require.ErrorIs(t, result.Err(), context.Canceled)
	require.False(t, result.Usage.Complete)
}

func TestClassifyBoundsConcurrentCalls(t *testing.T) {
	t.Parallel()
	var active, maximum atomic.Int32
	ready := make(chan struct{}, 1)
	release := make(chan struct{})
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); n > old; old = maximum.Load() {
			if maximum.CompareAndSwap(old, n) {
				break
			}
		}
		if n == 4 {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
		<-release
		var req wireRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			w.WriteHeader(400)
			return
		}
		answers := make(map[string]any)
		for key := range req.Questions {
			answers[key] = map[string]any{"type": "noul", "noul": 0.5}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": model, "answers": answers, "usage": map[string]int{"input_tokens": 10, "output_tokens": 1}})
	})
	req := classifier.NewRequest(classifier.Text("state"))
	for i := range 8 {
		req.Ask(classifier.Noul(classifier.QuestionKey(fmt.Sprint(i)), classifier.Text(strings.Repeat("x", 33000))))
	}
	done := make(chan classifier.Result, 8)
	for range 8 {
		go func() { done <- c.Classify(t.Context(), req) }()
	}
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("four provider requests did not start")
	}
	close(release)
	for range 8 {
		got := <-done
		require.NoError(t, got.Err())
		require.Len(t, got.Outcomes, 8)
		for i, outcome := range got.Outcomes {
			require.Equal(t, req.Questions[i].Key, outcome.Key)
			require.NotNil(t, outcome.Answer)
		}
		require.Equal(t, int64(10), got.Usage.InputTokens)
	}
	require.Equal(t, int32(4), maximum.Load())
}

func TestDecodeAnswerRejectsInvalidDistributions(t *testing.T) {
	t.Parallel()
	q := classifier.Choice("q", classifier.Text("choose"), classifier.NewOption("a", classifier.Text("A")), classifier.NewOption("b", classifier.Text("B")))
	compiled, err := compile(q)
	require.NoError(t, err)
	planned := plannedQuestion{index: 0, question: q, wire: compiled}
	for _, raw := range []string{
		`{"type":"choice","choice":"0","probabilities":{"0":0.1,"1":0.9},"confidence":0.5}`,
		`{"type":"choice","choice":"0","probabilities":{"0":0.9},"confidence":0.5}`,
		`{"type":"choice","choice":"0","probabilities":{"0":0.9,"1":null},"confidence":0.5}`,
		`{"type":"choice","choice":"0","probabilities":{"0":0.9,"1":0.5},"confidence":0.5}`,
		`{"type":"choice","choice":"0","probabilities":{"0":0.9,"1":0.1},"confidence":2}`,
		`{"type":"score","score":0.1,"probabilities":{"0":0.9,"1":0.1},"confidence":0.5}`,
	} {
		var answer wireAnswer
		require.NoError(t, json.Unmarshal([]byte(raw), &answer))
		require.Nil(t, decodeAnswer(planned, answer), raw)
	}
}
