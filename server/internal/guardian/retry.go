package guardian

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// retriesExhaustedBodyLimit bounds how much of the final response body a
// RetriesExhaustedError captures. Provider error bodies are small JSON
// documents; anything larger is truncated so the error stays loggable.
const retriesExhaustedBodyLimit = 1024

// RetriesExhaustedError is returned by clients built with a retry config
// when the client gives up on a request. Unlike retryablehttp's default
// "giving up" error, it preserves what the final attempt actually saw — the
// HTTP status and a snippet of the response body, or the transport error —
// so callers can tell a provider outage (persistent 429/5xx) from a
// misconfigured request without re-issuing it. The http.Client transport
// wraps it in a *url.Error; match with errors.As.
//
// Attempts counts attempts actually made: a value of 1 means the retry
// policy refused to retry at all (e.g. a TLS verification failure or a
// resilience denial), not that the budget ran out — check it before
// treating the error as evidence of a persistent upstream failure. Context
// cancellation and deadline expiry are never wrapped in this type.
type RetriesExhaustedError struct {
	// Method and URL identify the request that was given up on. URL has
	// any userinfo password redacted.
	Method string
	URL    string
	// Attempts is the number of attempts made before giving up.
	Attempts int
	// StatusCode is the HTTP status of the final attempt, or zero when the
	// final attempt failed before receiving a response.
	StatusCode int
	// Body is a truncated snippet of the final attempt's response body with
	// control characters replaced, empty when no response was received. It
	// is upstream-controlled content: safe to log, but do not surface it to
	// end users without an explicit decision to.
	Body string
	// Err is the transport error from the final attempt, if any.
	Err error
}

func (e *RetriesExhaustedError) Error() string {
	msg := fmt.Sprintf("giving up after %d attempt(s)", e.Attempts)
	if e.Method != "" {
		msg = fmt.Sprintf("%s %s %s", e.Method, e.URL, msg)
	}
	if e.StatusCode != 0 {
		msg = fmt.Sprintf("%s: last status %d", msg, e.StatusCode)
		if e.Body != "" {
			msg = fmt.Sprintf("%s: %s", msg, e.Body)
		}
	}
	if e.Err != nil {
		msg = fmt.Sprintf("%s: %s", msg, e.Err)
	}
	return msg
}

func (e *RetriesExhaustedError) Unwrap() error { return e.Err }

// retriesExhaustedErrorHandler converts a given-up request into a
// *RetriesExhaustedError. retryablehttp invokes the handler on every
// failing exit from Do — exhausted budgets, but also first-attempt aborts
// where the retry policy declined to retry — and hands it the final
// response un-drained, so the handler owns closing it. Caveat for future
// configs: if PrepareRetry is ever set and fails, retryablehttp has already
// drained and closed the response it passes here, so Body reads empty and
// StatusCode may be stale; no current config sets PrepareRetry.
func retriesExhaustedErrorHandler(resp *http.Response, err error, numTries int) (*http.Response, error) {
	// A canceled or expired context is the caller aborting, not the
	// upstream failing; pass it through so cancellation never masquerades
	// as an upstream error.
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		if resp != nil {
			_ = resp.Body.Close()
		}
		return nil, err
	}

	exhausted := &RetriesExhaustedError{
		Method:     "",
		URL:        "",
		Attempts:   numTries,
		StatusCode: 0,
		Body:       "",
		Err:        err,
	}
	if resp != nil {
		exhausted.Method = resp.Request.Method
		exhausted.URL = resp.Request.URL.Redacted()
		exhausted.StatusCode = resp.StatusCode
		body, _ := io.ReadAll(io.LimitReader(resp.Body, retriesExhaustedBodyLimit))
		_ = resp.Body.Close()
		exhausted.Body = printableBodySnippet(body)
	}
	return nil, exhausted
}

// printableBodySnippet makes an upstream body safe to embed in a
// single-line error string: control characters (including newlines) become
// spaces and invalid UTF-8 is replaced during the conversion.
func printableBodySnippet(body []byte) string {
	return strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, string(body))
}
