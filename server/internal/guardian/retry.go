package guardian

import (
	"fmt"
	"io"
	"net/http"
)

// retriesExhaustedBodyLimit bounds how much of the final response body a
// RetriesExhaustedError captures. Provider error bodies are small JSON
// documents; anything larger is truncated so the error stays loggable.
const retriesExhaustedBodyLimit = 1000

// RetriesExhaustedError is returned by clients built with a retry config
// once the whole retry budget is spent. Unlike retryablehttp's default
// "giving up" error, it preserves what the final attempt actually saw — the
// HTTP status and a snippet of the response body, or the transport error —
// so callers can tell a provider outage (persistent 429/5xx) from a
// misconfigured request without re-issuing it. The http.Client transport
// wraps it in a *url.Error; match with errors.As.
type RetriesExhaustedError struct {
	// Method and URL identify the request that ran out of retries.
	Method string
	URL    string
	// Attempts is the number of attempts made before giving up.
	Attempts int
	// StatusCode is the HTTP status of the final attempt, or zero when the
	// final attempt failed before receiving a response.
	StatusCode int
	// Body is a truncated snippet of the final attempt's response body,
	// empty when no response was received.
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

// retriesExhaustedErrorHandler converts retry exhaustion into a
// *RetriesExhaustedError. retryablehttp hands the handler the final
// response un-drained, so the handler owns closing it.
func retriesExhaustedErrorHandler(resp *http.Response, err error, numTries int) (*http.Response, error) {
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
		exhausted.URL = resp.Request.URL.String()
		exhausted.StatusCode = resp.StatusCode
		body, _ := io.ReadAll(io.LimitReader(resp.Body, retriesExhaustedBodyLimit))
		_ = resp.Body.Close()
		exhausted.Body = string(body)
	}
	return nil, exhausted
}
