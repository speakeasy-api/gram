package risk_analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/semaphore"
)

func TestStubPIIScannerReturnsEmptyResults(t *testing.T) {
	t.Parallel()

	results, err := (&StubPIIScanner{}).AnalyzeBatch(t.Context(), []string{"one", "two"}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, findings := range results {
		assert.Empty(t, findings)
	}
}

// TestPresidioClientSingleAttemptPerCall confirms that AnalyzeBatch issues
// exactly one HTTP request and surfaces non-200 responses verbatim. No
// internal retry, no bisect: the retry budget lives in RetryingPIIScanner.
func TestPresidioClientSingleAttemptPerCall(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "presidio down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	client := NewPresidioClient(srv.URL, otel.GetTracerProvider(), otel.GetMeterProvider(), testLogger(t))

	results, err := client.AnalyzeBatch(t.Context(), []string{"one"}, nil, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "presidio returned status 503")
	require.Len(t, results, 1)
	assert.Empty(t, results[0])
	assert.Equal(t, int64(1), calls.Load(), "presidio client must not retry internally")
}

// TestPresidioClientShortCircuitsOnAllEmptyTexts asserts the client skips the
// HTTP round-trip (and the byte semaphore) when every input is the empty
// string — Presidio would either 500 or return no findings, so the work is
// wasted.
func TestPresidioClientShortCircuitsOnAllEmptyTexts(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
	}))
	t.Cleanup(srv.Close)

	client := NewPresidioClient(srv.URL, otel.GetTracerProvider(), otel.GetMeterProvider(), testLogger(t))

	results, err := client.AnalyzeBatch(t.Context(), []string{"", "", ""}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 3)
	for _, r := range results {
		assert.Empty(t, r)
	}
	assert.Equal(t, int64(0), calls.Load(), "presidio /analyze must not be called when every input is empty")
}

// TestPresidioClientRequestPayload confirms the client emits a single
// /analyze POST containing all texts in the input slice exactly as given.
func TestPresidioClientRequestPayload(t *testing.T) {
	t.Parallel()

	var got presidioRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(make([][]presidioResult, len(got.Text))))
	}))
	t.Cleanup(srv.Close)

	client := NewPresidioClient(srv.URL, otel.GetTracerProvider(), otel.GetMeterProvider(), testLogger(t))

	results, err := client.AnalyzeBatch(t.Context(), []string{"alpha", "beta"}, []string{"EMAIL_ADDRESS"}, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, []string{"alpha", "beta"}, got.Text)
	assert.Equal(t, []string{"EMAIL_ADDRESS"}, got.Entities)
	assert.Equal(t, "en", got.Language)
}

// TestPresidioClientThrottleFiresHeartbeatWhileBlocked drains the byte budget
// before issuing the request so AnalyzeBatch must spin in the throttle wait
// loop. The test asserts that onProgress fires before the request unblocks.
func TestPresidioClientThrottleFiresHeartbeatWhileBlocked(t *testing.T) {
	t.Parallel()

	var serverHit atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverHit.Add(1)
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode([][]presidioResult{{}}))
	}))
	t.Cleanup(srv.Close)

	client := NewPresidioClient(srv.URL, otel.GetTracerProvider(), otel.GetMeterProvider(), testLogger(t))
	// Shrink the throttle so the test deterministically blocks on a tiny
	// payload, and tighten the heartbeat interval so onProgress fires fast.
	client.throttle = semaphore.NewWeighted(4)
	client.throttleBudget = 4
	client.throttleHeartbeat = 5 * time.Millisecond

	// Hold the entire budget so the AnalyzeBatch call cannot acquire.
	require.True(t, client.throttle.TryAcquire(4))

	var progress atomic.Int64
	callResult := make(chan callOutcome, 1)

	go func() {
		results, err := client.AnalyzeBatch(t.Context(), []string{"hello"}, nil, func() {
			progress.Add(1)
		})
		callResult <- callOutcome{results: results, err: err}
	}()

	require.Eventually(t, func() bool { return progress.Load() >= 2 }, time.Second, 5*time.Millisecond,
		"onProgress did not fire while waiting on the byte semaphore")

	client.throttle.Release(4)

	outcome := <-callResult
	require.NoError(t, outcome.err)
	require.Len(t, outcome.results, 1)
	assert.Equal(t, int64(1), serverHit.Load(), "expected exactly one HTTP request after throttle release")
}

type callOutcome struct {
	results [][]Finding
	err     error
}

// TestRetryingPIIScannerRetriesThenSucceeds verifies the per-text retry budget
// is honored and the scanner returns real findings once Presidio recovers.
func TestRetryingPIIScannerRetriesThenSucceeds(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 2 {
			http.Error(w, "presidio down", http.StatusServiceUnavailable)
			return
		}
		var req presidioRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		results := make([][]presidioResult, len(req.Text))
		for i, text := range req.Text {
			if idx := strings.Index(text, "alice@example.com"); idx >= 0 {
				results[i] = []presidioResult{{
					EntityType: "EMAIL_ADDRESS",
					Start:      idx,
					End:        idx + len("alice@example.com"),
					Score:      1,
				}}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(results))
	}))
	t.Cleanup(srv.Close)

	scanner := newTestRetryingScanner(t, srv.URL)
	results, err := scanner.AnalyzeBatch(t.Context(), []string{"contact alice@example.com"}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Len(t, results[0], 1)
	assert.Equal(t, "alice@example.com", results[0][0].Match)
	assert.Empty(t, results[0][0].DeadLetterReason)
	assert.GreaterOrEqual(t, hits.Load(), int64(2), "expected at least one retry before success")
}

// TestRetryingPIIScannerDeadLettersAfterExhausting validates the orchestrator
// emits a DL sentinel after maxAttempts failures rather than surfacing the
// error to the caller. Logs the per-text size so post-incident triage can
// recover what failed.
func TestRetryingPIIScannerDeadLettersAfterExhausting(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "still down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	scanner := newTestRetryingScanner(t, srv.URL)
	results, err := scanner.AnalyzeBatch(t.Context(), []string{"will be dead-lettered"}, nil, nil)
	require.NoError(t, err, "per-text failures must not bubble up as activity-layer errors")
	require.Len(t, results, 1)
	require.Len(t, results[0], 1)

	dl := results[0][0]
	assert.Equal(t, SourcePresidio, dl.Source)
	assert.Equal(t, DeadLetterRuleID, dl.RuleID)
	assert.NotEmpty(t, dl.DeadLetterReason)
	assert.Equal(t, int64(retryingMaxAttempts), hits.Load())
}

// TestRetryingPIIScannerIsolatesPoisonedMessages confirms that a single
// poisoned message dead-letters without affecting its batch siblings — the
// failure mode the old bisecting client could not cleanly handle.
func TestRetryingPIIScannerIsolatesPoisonedMessages(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		var req presidioRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		// Every call carries exactly one text under the per-message fan-out.
		assert.Len(t, req.Text, 1)
		if len(req.Text) > 0 && req.Text[0] == "poison" {
			http.Error(w, "poison rejected", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(make([][]presidioResult, len(req.Text))))
	}))
	t.Cleanup(srv.Close)

	scanner := newTestRetryingScanner(t, srv.URL)
	results, err := scanner.AnalyzeBatch(t.Context(), []string{"clean a", "poison", "clean b"}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 3)

	assert.Empty(t, results[0])
	assert.Empty(t, results[2])

	require.Len(t, results[1], 1)
	assert.Equal(t, SourcePresidio, results[1][0].Source)
	assert.Equal(t, DeadLetterRuleID, results[1][0].RuleID)
	assert.NotEmpty(t, results[1][0].DeadLetterReason)
}

// TestRetryingPIIScannerSurfacesOuterContextCancellation asserts that an
// outer-ctx cancellation aborts cleanly and returns an error so the Temporal
// activity layer can retry the whole batch rather than treating partial
// results as final.
func TestRetryingPIIScannerSurfacesOuterContextCancellation(t *testing.T) {
	t.Parallel()

	// Server blocks until either the request's own ctx is cancelled or the
	// test releases it. Keep-alive connection reuse means the client-side
	// ctx-cancel does not always propagate to r.Context().Done() before the
	// test tears down, so an explicit release channel keeps srv.Close from
	// hanging on the in-flight handler. LIFO cleanup: release first, then
	// Close.
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-released:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(released) })

	scanner := newTestRetryingScanner(t, srv.URL)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel before the call so the first ctx.Err() check trips

	_, err := scanner.AnalyzeBatch(ctx, []string{"hang"}, nil, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

// TestRetryingPIIScannerDeadLettersOnPerRequestTimeout confirms that
// transient inner-timeouts consume the retry budget rather than bailing
// early — once exhausted the message dead-letters with the underlying
// deadline-exceeded error captured.
func TestRetryingPIIScannerDeadLettersOnPerRequestTimeout(t *testing.T) {
	t.Parallel()

	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-released:
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(released) })

	scanner := newTestRetryingScanner(t, srv.URL)
	// Shrink per-request timeout so the test exercises the retry path
	// without waiting out the 30s production default.
	scanner.client.requestTimeout = 30 * time.Millisecond

	results, err := scanner.AnalyzeBatch(t.Context(), []string{"hang"}, nil, nil)
	require.NoError(t, err, "inner per-request timeouts must not bubble up as activity-layer errors")
	require.Len(t, results, 1)
	require.Len(t, results[0], 1)

	dl := results[0][0]
	assert.Equal(t, SourcePresidio, dl.Source)
	assert.Equal(t, DeadLetterRuleID, dl.RuleID)
	assert.NotEmpty(t, dl.DeadLetterReason)
}

// TestComputeRetryBackoffStaysWithinCap asserts the jittered exponential
// backoff is bounded so a stuck Presidio can't blow the activity heartbeat.
func TestComputeRetryBackoffStaysWithinCap(t *testing.T) {
	t.Parallel()

	for attempt := range 10 {
		got := computeRetryBackoff(50*time.Millisecond, attempt)
		assert.GreaterOrEqual(t, got, time.Duration(0))
		assert.LessOrEqual(t, got, retryingMaxBackoff)
	}
	assert.Zero(t, computeRetryBackoff(0, 5))
}

// TestRequestByteCostCapsToBudget guards the deadlock-avoidance branch: a
// fresh client whose semaphore has the full budget free must still be able
// to issue a single-message request larger than the budget.
func TestRequestByteCostCapsToBudget(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("a", int(presidioInflightByteBudget)*2)
	assert.Equal(t, presidioInflightByteBudget, requestByteCost([]string{big}, presidioInflightByteBudget))
	assert.Equal(t, int64(1), requestByteCost([]string{""}, presidioInflightByteBudget))
	assert.Equal(t, int64(7), requestByteCost([]string{"abc", "defg"}, presidioInflightByteBudget))
}

func TestIsCancelErrClassifiesContextErrors(t *testing.T) {
	t.Parallel()

	assert.True(t, isCancelErr(context.Canceled))
	assert.True(t, isCancelErr(context.DeadlineExceeded))
	assert.True(t, isCancelErr(fmt.Errorf("wrapped: %w", context.Canceled)))
	assert.True(t, isCancelErr(fmt.Errorf("wrapped: %w", context.DeadlineExceeded)))
	assert.False(t, isCancelErr(nil))
	assert.False(t, isCancelErr(errors.New("presidio returned status 500")))
}

// --- helpers ---

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(t.Output(), nil))
}

func newTestRetryingScanner(t *testing.T, baseURL string) *RetryingPIIScanner {
	t.Helper()
	client := NewPresidioClient(baseURL, otel.GetTracerProvider(), otel.GetMeterProvider(), testLogger(t))
	scanner := NewRetryingPIIScanner(client, otel.GetTracerProvider(), otel.GetMeterProvider(), testLogger(t))
	// Zero backoff keeps tests deterministic; retry budget stays at the
	// production default.
	scanner.baseBackoff = 0
	return scanner
}
