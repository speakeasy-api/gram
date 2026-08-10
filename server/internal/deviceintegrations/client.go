package deviceintegrations

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/speakeasy-api/gram/server/internal/deviceintegrations/providers"
	"github.com/speakeasy-api/gram/server/internal/guardian"
)

// syncErrorMaxLen truncates stored provider errors: last_poll_error and the
// testConnection message are dashboard surfaces, not log sinks.
const syncErrorMaxLen = 500

// boundedProviderClient is the SSRF-hardened guardian client with a
// redirect cap layered on: a hostile vendor must not chase arbitrary
// redirect chains (mirrors remotemcp's probe bounds; response-size limits
// are each provider implementation's job). A tight Retry-After-aware retry
// smooths transient vendor throttling (429) and blips (5xx): without it a
// single throttled request aborts a whole sync run, and for evidence pushes
// the schedule-level retry restarts the entire multi-request push from
// scratch — inflating the very rate-limit pressure that caused the failure.
// The caps stay small because every vendor call already runs under the sync
// runner's per-call deadline.
func boundedProviderClient(policy *guardian.Policy) *guardian.HTTPClient {
	client := policy.Client(guardian.WithRetryConfig(&guardian.RetryConfig{
		WaitMin:     1 * time.Second,
		WaitMax:     10 * time.Second,
		MaxAttempts: 3,
		CheckRetry:  retryablehttp.DefaultRetryPolicy,
		// DefaultBackoff honors a 429/503 Retry-After header VERBATIM,
		// ignoring WaitMax — a vendor asking for minutes would stall the
		// sync run inside one attempt. Clamp it: a wait beyond the cap
		// means giving up and letting the schedule's own backoff handle it.
		Backoff: func(minWait, maxWait time.Duration, attemptNum int, resp *http.Response) time.Duration {
			delay := retryablehttp.DefaultBackoff(minWait, maxWait, attemptNum, resp)
			if delay > maxWait {
				return maxWait
			}
			return delay
		},
		ErrorHandler: nil,
		PrepareRetry: nil,
	}))
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return fmt.Errorf("stopped after 3 redirects")
		}
		return nil
	}
	return client
}

// sanitizeSyncError scrubs credential values out of a provider error and
// truncates it rune-safely: vendor transport errors can echo request URLs,
// including URL-escaped forms of a secret. The scrub threshold is guaranteed
// to cover every stored secret because upsert validation enforces
// minSecretLength.
func sanitizeSyncError(message string, creds providers.Credentials) string {
	for _, value := range creds {
		if len(value) < 4 {
			continue
		}
		for _, form := range []string{value, url.QueryEscape(value), url.PathEscape(value)} {
			message = strings.ReplaceAll(message, form, "[redacted]")
		}
	}
	if len(message) > syncErrorMaxLen {
		message = strings.ToValidUTF8(message[:syncErrorMaxLen], "")
	}
	return message
}
