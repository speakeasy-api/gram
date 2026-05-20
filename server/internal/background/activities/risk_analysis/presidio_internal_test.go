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

// TestConvertPresidioFindings_FiltersIPv6Unspecified verifies that the
// IPv6 unspecified address (`::` and its all-zero variants) — which
// Presidio aggressively flags wherever it appears — is dropped before
// becoming a finding. Real IPv6 addresses still flow through.
func TestConvertPresidioFindings_FiltersIPv6Unspecified(t *testing.T) {
	t.Parallel()

	text := ":: and ::0 and 2001:db8::1"
	results := []presidioResult{
		{EntityType: "IP_ADDRESS", Start: 0, End: 2, Score: 0.9},   // "::"
		{EntityType: "IP_ADDRESS", Start: 7, End: 10, Score: 0.9},  // "::0"
		{EntityType: "IP_ADDRESS", Start: 15, End: 26, Score: 0.9}, // "2001:db8::1"
	}

	findings := convertPresidioFindings(text, results)
	require.Len(t, findings, 1, "only the real IPv6 address should survive the filter")
	assert.Equal(t, "2001:db8::1", findings[0].Match)
}

func TestIsPresidioFalsePositive_OnlyIPAddressUnspecified(t *testing.T) {
	t.Parallel()

	// Unspecified addresses are filtered, IPv6 and IPv4.
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "::"))
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "::0"))
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "0::0"))
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "0:0:0:0:0:0:0:0"))
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "0.0.0.0"))
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "  ::  "), "trimmed")

	// Loopback addresses are filtered, IPv6 and IPv4 (whole 127.0.0.0/8).
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "127.0.0.1"))
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "127.1.2.3"))
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "::1"))

	// IPv6 short-form "<hex>::" patterns dominate Presidio's IP_ADDRESS
	// noise on prod (hex constants and text fragments greedily matched).
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "b::"))
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "dead::"))
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "1::"))
	assert.True(t, isPresidioFalsePositive("IP_ADDRESS", "DEAF::"), "case-insensitive")

	// Real addresses are not filtered.
	assert.False(t, isPresidioFalsePositive("IP_ADDRESS", "8.8.8.8"))
	assert.False(t, isPresidioFalsePositive("IP_ADDRESS", "2001:db8::1"))
	assert.False(t, isPresidioFalsePositive("IP_ADDRESS", "dead::beef"), "two-group IPv6 still real")

	// Other entity types are never filtered by this rule.
	assert.False(t, isPresidioFalsePositive("EMAIL_ADDRESS", "::"))
}

func TestStubPIIScannerReturnsEmptyResults(t *testing.T) {
	t.Parallel()

	results, err := (&StubPIIScanner{}).AnalyzeBatch(t.Context(), []string{"one", "two"}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, findings := range results {
		assert.Empty(t, findings)
	}
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

// TestAnalyzeOnceSingleAttemptNoRetry asserts the inner single-attempt
// method does NOT retry on failure — retry lives one level up in
// analyzeOne. This is the test that justifies keeping analyzeOnce as a
// separate seam under analyzeOne.
func TestAnalyzeOnceSingleAttemptNoRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "presidio down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	client := newTestPresidioClient(t, srv.URL)
	_, err := client.analyzeOnce(t.Context(), "one", nil, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "presidio returned status 503")
	assert.Equal(t, int64(1), calls.Load(), "analyzeOnce must not retry internally")
}

// TestAnalyzeOnceRequestPayload confirms the inner single-attempt method
// emits one /analyze POST carrying the text in a one-element array and
// passes the requested entities + language through verbatim.
func TestAnalyzeOnceRequestPayload(t *testing.T) {
	t.Parallel()

	var got presidioRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode([][]presidioResult{{}}))
	}))
	t.Cleanup(srv.Close)

	client := newTestPresidioClient(t, srv.URL)
	_, err := client.analyzeOnce(t.Context(), "alpha", []string{"EMAIL_ADDRESS"}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha"}, got.Text)
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

	client := newTestPresidioClient(t, srv.URL)
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

// TestPresidioClientRetriesThenSucceeds verifies the per-text retry budget
// is honored and the scanner returns real findings once Presidio recovers.
func TestPresidioClientRetriesThenSucceeds(t *testing.T) {
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

	client := newTestPresidioClient(t, srv.URL)
	results, err := client.AnalyzeBatch(t.Context(), []string{"contact alice@example.com"}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Len(t, results[0], 1)
	assert.Equal(t, "alice@example.com", results[0][0].Match)
	assert.Empty(t, results[0][0].DeadLetterReason)
	assert.GreaterOrEqual(t, hits.Load(), int64(2), "expected at least one retry before success")
}

// TestPresidioClientDeadLettersAfterExhausting validates the retry budget
// emits a DL sentinel after maxAttempts failures rather than surfacing the
// error to the caller. Logs the per-text size so post-incident triage can
// recover what failed.
func TestPresidioClientDeadLettersAfterExhausting(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "still down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	client := newTestPresidioClient(t, srv.URL)
	results, err := client.AnalyzeBatch(t.Context(), []string{"will be dead-lettered"}, nil, nil)
	require.NoError(t, err, "per-text failures must not bubble up as activity-layer errors")
	require.Len(t, results, 1)
	require.Len(t, results[0], 1)

	dl := results[0][0]
	assert.Equal(t, SourcePresidio, dl.Source)
	assert.Equal(t, DeadLetterRuleID, dl.RuleID)
	assert.NotEmpty(t, dl.DeadLetterReason)
	assert.Equal(t, int64(retryMaxAttempts), hits.Load())
}

// TestPresidioClientIsolatesPoisonedMessages confirms that a single
// poisoned message dead-letters without affecting its batch siblings — the
// failure mode the old bisecting client could not cleanly handle.
func TestPresidioClientIsolatesPoisonedMessages(t *testing.T) {
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

	client := newTestPresidioClient(t, srv.URL)
	results, err := client.AnalyzeBatch(t.Context(), []string{"clean a", "poison", "clean b"}, nil, nil)
	require.NoError(t, err)
	require.Len(t, results, 3)

	assert.Empty(t, results[0])
	assert.Empty(t, results[2])

	require.Len(t, results[1], 1)
	assert.Equal(t, SourcePresidio, results[1][0].Source)
	assert.Equal(t, DeadLetterRuleID, results[1][0].RuleID)
	assert.NotEmpty(t, results[1][0].DeadLetterReason)
}

// TestPresidioClientSurfacesOuterContextCancellation asserts that an
// outer-ctx cancellation aborts cleanly and returns an error so the Temporal
// activity layer can retry the whole batch rather than treating partial
// results as final.
func TestPresidioClientSurfacesOuterContextCancellation(t *testing.T) {
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

	client := newTestPresidioClient(t, srv.URL)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel before the call so the first ctx.Err() check trips

	_, err := client.AnalyzeBatch(ctx, []string{"hang"}, nil, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
}

// TestPresidioClientDeadLettersOnPerRequestTimeout confirms that transient
// inner-timeouts consume the retry budget rather than bailing early — once
// exhausted the message dead-letters with the underlying deadline-exceeded
// error captured.
func TestPresidioClientDeadLettersOnPerRequestTimeout(t *testing.T) {
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

	client := newTestPresidioClient(t, srv.URL)
	// Shrink per-request timeout so the test exercises the retry path
	// without waiting out the 30s production default.
	client.requestTimeout = 30 * time.Millisecond

	results, err := client.AnalyzeBatch(t.Context(), []string{"hang"}, nil, nil)
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
		assert.LessOrEqual(t, got, retryMaxBackoff)
	}
	assert.Zero(t, computeRetryBackoff(0, 5))
}

// TestRequestByteCostCapsToBudget guards the deadlock-avoidance branch: a
// fresh client whose semaphore has the full budget free must still be able
// to issue a single-message request larger than the budget.
func TestRequestByteCostCapsToBudget(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("a", presidioMaxMessageBytes*2)
	assert.Equal(t, int64(presidioMaxMessageBytes), requestByteCost(big, presidioMaxMessageBytes))
	assert.Equal(t, int64(1), requestByteCost("", presidioMaxMessageBytes))
	assert.Equal(t, int64(4), requestByteCost("defg", presidioMaxMessageBytes))
}

// TestPresidioClientTruncatesOversizedMessages confirms the client clips a
// message larger than presidioMaxMessageBytes to a UTF-8 boundary before
// sending so a single fat blob can't crash Presidio (1 MB payloads have
// been observed to kill the analyzer). The truncated payload is what
// reaches the server; the original size is captured in logs/metrics.
func TestPresidioClientTruncatesOversizedMessages(t *testing.T) {
	t.Parallel()

	var receivedSize int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req presidioRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		if assert.Len(t, req.Text, 1) {
			receivedSize = len(req.Text[0])
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode([][]presidioResult{{}}))
	}))
	t.Cleanup(srv.Close)

	client := newTestPresidioClient(t, srv.URL)
	// Build an input that is double the limit and contains a multibyte rune
	// straddling the truncation point so we exercise the UTF-8 walk-back.
	body := strings.Repeat("a", presidioMaxMessageBytes-1) + "€" + strings.Repeat("b", presidioMaxMessageBytes)
	_, err := client.AnalyzeBatch(t.Context(), []string{body}, nil, nil)
	require.NoError(t, err)

	// Truncation walks back to a rune start, so we land strictly inside the
	// cap (the "€" occupies 3 bytes starting at presidioMaxMessageBytes-1).
	assert.LessOrEqual(t, receivedSize, presidioMaxMessageBytes)
	assert.Greater(t, receivedSize, presidioMaxMessageBytes-4, "expected truncation near the cap, not further back")
}

func TestTruncateAtRuneBoundaryHandlesMultibyte(t *testing.T) {
	t.Parallel()

	// "€" is 3 bytes in UTF-8. Cap at the middle byte: walk-back must land
	// before the rune starts so the suffix is well-formed UTF-8.
	in := "aa€bb"
	assert.Equal(t, "aa", truncateAtRuneBoundary(in, 3))
	assert.Equal(t, "aa", truncateAtRuneBoundary(in, 4))
	assert.Equal(t, "aa€", truncateAtRuneBoundary(in, 5))
	assert.Equal(t, in, truncateAtRuneBoundary(in, 100))
	assert.Empty(t, truncateAtRuneBoundary(in, 0))
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

func newTestPresidioClient(t *testing.T, baseURL string) *PresidioClient {
	t.Helper()
	client := NewPresidioClient(baseURL, otel.GetTracerProvider(), otel.GetMeterProvider(), testLogger(t))
	// Zero backoff keeps tests deterministic; retry budget stays at the
	// production default so retry-related assertions stay representative.
	client.baseBackoff = 0
	return client
}
