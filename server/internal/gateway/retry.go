package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"slices"
	"strconv"
	"syscall"
	"time"
)

// retryConfig is the policy retryWithBackoff applies to a proxied request: how
// many attempts to make, how long to wait between them, and which responses are
// safe to retry. Build one with httpToolRetryConfig or functionRunnerRetryConfig
// rather than assembling it inline, so the retryable-status rules stay in one
// place.
type retryConfig struct {
	// backoffFactor is the multiplier applied to the wait after each attempt,
	// producing exponential growth (e.g. 2 doubles the interval each time).
	backoffFactor float64

	// initialInterval is the base wait before the first retry. It is the floor
	// the first backoff is computed from and grows by backoffFactor on each
	// subsequent attempt. A server Retry-After header overrides it for that
	// attempt (capped at maxInterval).
	initialInterval time.Duration

	// maxAttempts is the total number of attempts, including the first. Values
	// below 1 are clamped to 1 by retryWithBackoff so the request always runs at
	// least once.
	maxAttempts int

	// maxInterval caps the backoff wait — both the exponentially grown interval
	// and any honored Retry-After — before per-attempt jitter is layered on.
	maxInterval time.Duration

	// retryableStatus maps an HTTP request method to the response status codes
	// that are safe to retry for that method. A method absent from the map is
	// never retried on status (only on pre-execution transport errors). Keying by
	// method lets idempotent GETs retry a broad transient-status preset while
	// non-idempotent POSTs (function tool calls / resource reads) retry only the
	// narrow set that is provably pre-execution, so they are never re-executed.
	retryableStatus map[string][]int
}

// functionRunnerRetryConfig is the retry policy for calls to the Fly functions
// runner (tool calls and resource reads, always POST). The runner sheds an
// over-capacity request with 429 + Retry-After *before* executing it, and Fly
// returns 503 when the fleet is full and never delivered the request to a
// runner, so both are provably pre-execution and safe to retry even though the
// request is a non-idempotent POST. 500/502/504 are deliberately excluded: those
// may be post-execution, so retrying them risks double-execution.
func functionRunnerRetryConfig() retryConfig {
	return retryConfig{
		initialInterval: 1 * time.Second,
		maxInterval:     5 * time.Second,
		maxAttempts:     3,
		backoffFactor:   2,
		retryableStatus: map[string][]int{
			http.MethodPost: {
				http.StatusTooManyRequests,    // 429: runner limiter shed it pre-execution
				http.StatusServiceUnavailable, // 503: Fly fleet full, never delivered
			},
		},
	}
}

// httpToolRetryConfig is the retry policy for outbound HTTP tool calls. Only
// idempotent GETs retry, on a broad transient-status preset; non-idempotent
// methods (POST/PUT/...) are never retried on status because an upstream 429/503
// is not provably pre-execution for arbitrary APIs, so retrying risks
// double-execution.
func httpToolRetryConfig() retryConfig {
	return retryConfig{
		// Space retries so the later attempts land after a Fly machine cold start
		// (~2s) has had time to complete, without adding more attempts (which would
		// only pile load onto an already-saturated runner). With backoffFactor 2
		// the base waits are 1s then 2s, before per-attempt jitter is layered on.
		initialInterval: 1 * time.Second,
		maxInterval:     5 * time.Second,
		maxAttempts:     3,
		backoffFactor:   2,
		retryableStatus: map[string][]int{
			http.MethodGet: {
				408, // Request Timeout
				429, // Rate Limit Exceeded
				500, // Internal Server Error
				502, // Bad Gateway
				503, // Service Unavailable
				504, // Gateway Timeout
				509, // Bandwidth Limit Exceeded
				521, // Web Server Is Down (Cloudflare)
				522, // Connection Timed Out (Cloudflare)
				523, // Origin Is Unreachable (Cloudflare)
				524, // A Timeout Occurred (Cloudflare)
			},
		},
	}
}

// isRetryableTransportError reports whether err returned from http.Client.Do is a
// connection-level failure that happened before the upstream processed the
// request. Retrying these is safe even for non-idempotent methods (e.g. function
// tool-call POSTs) because the request was not acted on. Caller cancellation and
// deadlines are excluded: the request may already be in flight, so retrying only
// wastes work.
func isRetryableTransportError(err error) bool {
	if err == nil {
		return false
	}

	// The caller gave up (timeout or cancellation); the request may already be in
	// flight, so do not retry.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// A bare io.EOF from client.Do means the connection closed during the
	// request/response-header exchange, before any response was received. For
	// Fly-hosted function runners this is overwhelmingly the edge closing the
	// connection before it can route to a machine that is stopped, stopping, or
	// not yet recognized as started, so the request was not processed. From the
	// error alone we cannot distinguish that from the rarer case where the runner
	// processed the request and then the connection dropped before responding;
	// retrying io.EOF accepts that residual double-execution risk. It is the best
	// achievable without per-tool idempotency keys, and the observed EOF traffic
	// is dominated by idempotent reads. io.ErrUnexpectedEOF stays excluded: a
	// partial response means the request was processed.
	if errors.Is(err, io.EOF) {
		return true
	}

	// Connection refused is always a failed connect: the request was never
	// delivered.
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}

	// Failures while establishing the connection (dial/connect), including a reset
	// or timeout before the request is written, are safe to retry. Resets while
	// reading or writing an in-flight request are intentionally NOT retried here,
	// to avoid re-executing a request the upstream may have already processed.
	if netErr, ok := errors.AsType[*net.OpError](err); ok {
		switch netErr.Op {
		case "dial", "connect":
			return true
		}
	}

	return false
}

// jitteredBackoff returns base plus a full-jittered spread uniform in [0, base),
// so the result is uniform in [base, 2*base). base acts as a floor (e.g. a
// runner Retry-After), and the added jitter spreads concurrent retries so a wave
// of simultaneously-throttled function calls does not retry in lockstep
// (thundering herd). Returns 0 when base is non-positive.
func jitteredBackoff(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	return base + time.Duration(rand.Int64N(int64(base))) // #nosec G404 retry jitter is not security-sensitive
}

func retryWithBackoff(
	ctx context.Context,
	retryBackoff retryConfig,
	doRequest func() (*http.Response, error),
) (*http.Response, error) {
	var resp *http.Response
	var err error
	// Always make at least one attempt, even if a caller supplies a zero-valued
	// config, so the request is never silently dropped without executing.
	maxAttempts := max(retryBackoff.maxAttempts, 1)
	delayInterval := retryBackoff.initialInterval

	for attempt := range maxAttempts {
		if attempt > 0 {
			// Discard the previous attempt's response before retrying: drain a bounded
			// amount so the keep-alive connection can be reused, then close the body so
			// it (and its connection) is not leaked across attempts. resp is nil after a
			// retried transport error, so guard it. The final, returned response is
			// never closed here — the loop exits without re-entering this block — so the
			// caller still receives an open body.
			if resp != nil {
				_, _ = io.CopyN(io.Discard, resp.Body, 64*1024)
				_ = resp.Body.Close()
			}

			// Jitter the wait (full jitter on top of delayInterval as a floor) so a
			// wave of simultaneously-throttled requests does not retry in lockstep.
			// delayInterval carries either the exponential backoff or a server
			// Retry-After (both capped at maxInterval), so the floor is respected on
			// every path while the spread is layered on here.
			select {
			case <-time.After(jitteredBackoff(delayInterval)):
			case <-ctx.Done():
				return nil, fmt.Errorf("retry context done: %w", ctx.Err())
			}

			delayInterval = min(time.Duration(float64(delayInterval)*retryBackoff.backoffFactor), retryBackoff.maxInterval)
		}
		resp, err = doRequest()
		if err != nil {
			// Only retry connection-level failures that happened before the
			// upstream processed the request, so non-idempotent requests (e.g.
			// function tool-call POSTs) are never re-executed.
			if isRetryableTransportError(err) {
				continue
			}

			return nil, err
		}
		retryableCodes, ok := retryBackoff.retryableStatus[resp.Request.Method]
		if !ok || !slices.Contains(retryableCodes, resp.StatusCode) {
			return resp, err
		}

		// Retry-After is either delta-seconds or an HTTP-date. Whichever form parses
		// sets delayInterval as the floor for the next (jittered) backoff, capped at
		// maxInterval; an unparseable, zero, or past value leaves the exponential
		// backoff in place. The two forms are mutually exclusive, so the date parse
		// runs only when the numeric parse does not match.
		if retryAfter := resp.Header.Get("retry-after"); retryAfter != "" {
			if parsedNumber, err := strconv.ParseInt(retryAfter, 10, 64); err == nil && parsedNumber > 0 {
				retryAfterDuration := time.Duration(parsedNumber) * time.Second
				delayInterval = min(retryAfterDuration, retryBackoff.maxInterval)
			} else if parsedDate, err := http.ParseTime(retryAfter); err == nil {
				retryAfterDuration := time.Until(parsedDate)
				if retryAfterDuration > 0 {
					delayInterval = min(retryAfterDuration, retryBackoff.maxInterval)
				}
			}
		}
	}
	return resp, err
}
