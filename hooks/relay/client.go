package relay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	sdk "github.com/speakeasy-api/gram/hooks/sdk"
	"github.com/speakeasy-api/gram/hooks/sdk/models/apierrors"
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
	"github.com/speakeasy-api/gram/hooks/sdk/models/operations"
	"github.com/speakeasy-api/gram/hooks/sdk/retry"
)

const perAttemptTime = 10 * time.Second

// sendBudget bounds one send end to end — the SDK's internal 30s retry budget
// and the transport replays below stack, and a gating hook that outlives the
// provider's 60s timeout fails closed uncontrolled instead of returning a
// verdict.
const sendBudget = 45 * time.Second

// retryMaxElapsedMS caps the SDK's backoff budget for retryable statuses
// (429/5xx). A var rather than a const so tests that script 5xx responses can
// shrink it below the wall clock they can afford.
var retryMaxElapsedMS = 30_000

// decision is the server's verdict for a hook event.
type decision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
}

func (d decision) denied() bool { return strings.EqualFold(d.Decision, "deny") }

// ingestResult reports the outcome of an ingest attempt.
type ingestResult struct {
	// statusCode is the final HTTP status, or 0 if the server was never
	// reached with a definitive response.
	statusCode int
	decision   decision
	// authRejected is true when the server rejected the credential (401/403).
	authRejected bool
	// failOpen carries the org's downtime posture from the response's
	// org_settings effects; nil when the server sent none.
	failOpen *bool
}

// client posts canonical hook events through the generated ingest SDK with
// bounded retries and a reused idempotency token so redelivered requests are
// stored exactly once.
type client struct {
	sdk *sdk.SpeakeasyHooks
	// budget caps one send end to end; a field so tests can shrink it.
	budget time.Duration
}

func newClient(serverURL string) *client {
	return &client{
		budget: sendBudget,
		sdk: sdk.New(
			sdk.WithServerURL(strings.TrimRight(serverURL, "/")),
			sdk.WithClient(&http.Client{Timeout: perAttemptTime}),
			// Retries cover connection errors and 429/5xx; the SDK rewinds the
			// request body per attempt, so the Idempotency-Key header minted in
			// send is reused across redeliveries. The elapsed cap keeps the
			// worst case well under the 60s gating-hook timeout.
			sdk.WithRetryConfig(retry.Config{
				Strategy: "backoff",
				Backoff: &retry.BackoffStrategy{
					InitialInterval: 1_000,
					MaxInterval:     4_000,
					Exponent:        1.5,
					MaxElapsedTime:  retryMaxElapsedMS,
				},
				RetryConnectionErrors: true,
			}),
		),
	}
}

// send posts the payload to the ingest endpoint authenticated with c. The
// SDK's built-in retries do not replay connection errors for POSTs, so pure
// transport failures (statusCode 0, the server was never reached) are replayed
// here — safe because the Idempotency-Key is minted once and reused, and
// necessary because a blocking hook would otherwise deny over one dropped
// connection.
func (cl *client) send(ctx context.Context, c creds, body components.IngestRequestBody) ingestResult {
	ctx, cancel := context.WithTimeout(ctx, cl.budget)
	defer cancel()

	req := operations.IngestHookEventRequest{
		GramKey:        new(c.APIKey),
		GramProject:    nil,
		IdempotencyKey: new(newIdempotencyToken()),
		Body:           body,
	}
	if c.Project != "" {
		req.GramProject = new(c.Project)
	}

	var res *operations.IngestHookEventResponse
	var err error
	for attempt := 0; ; attempt++ {
		res, err = cl.sdk.Hooks.Ingest(ctx, req)
		if err == nil {
			break
		}
		out := interpretError(err)
		if out.statusCode != 0 || attempt >= 2 || ctx.Err() != nil {
			return out
		}
		select {
		case <-ctx.Done():
			return out
		case <-time.After(time.Duration(attempt+1) * 250 * time.Millisecond):
		}
	}

	out := ingestResult{statusCode: res.StatusCode, decision: decision{Decision: "", Reason: "", Message: ""}, authRejected: false, failOpen: nil}
	if res.IngestHookResult != nil {
		out.decision = decision{
			Decision: string(res.IngestHookResult.Decision),
			Reason:   strDeref(res.IngestHookResult.Reason),
			Message:  strDeref(res.IngestHookResult.Message),
		}
		if settings, ok := res.IngestHookResult.Effects["org_settings"].(map[string]any); ok {
			if v, ok := settings["fail_open"].(bool); ok {
				out.failOpen = &v
			}
		}
	}
	return out
}

// interpretError maps an SDK error onto the relay's result semantics: a typed
// or generic API error carries a definitive status the ratchet can act on; a
// transport failure that never produced a response leaves statusCode 0.
func interpretError(err error) ingestResult {
	var svcErr *apierrors.ServiceError
	if errors.As(err, &svcErr) && svcErr.RawResponse != nil {
		status := svcErr.RawResponse.StatusCode
		return ingestResult{
			statusCode:   status,
			decision:     decision{Decision: "", Reason: svcErr.Name, Message: svcErr.Message},
			authRejected: status == http.StatusUnauthorized || status == http.StatusForbidden,
			failOpen:     nil,
		}
	}
	var apiErr *apierrors.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode > 0 {
		if apiErr.StatusCode >= 200 && apiErr.StatusCode < 300 {
			// A 2xx the SDK could not parse (wrong content type, unexpected
			// status) carries no verdict; passing the status through would
			// read as an implicit allow in evaluate. Report a failed exchange
			// instead — statusCode 0 also lets send replay it, and a duplicate
			// delivery is safe under the reused Idempotency-Key.
			return ingestResult{
				statusCode:   0,
				decision:     decision{Decision: "", Reason: "", Message: "Speakeasy hooks could not read the server's verdict."},
				authRejected: false,
				failOpen:     nil,
			}
		}
		return ingestResult{
			statusCode:   apiErr.StatusCode,
			decision:     decision{Decision: "", Reason: "", Message: ""},
			authRejected: apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden,
			failOpen:     nil,
		}
	}
	return ingestResult{statusCode: 0, decision: decision{Decision: "", Reason: "", Message: ""}, authRejected: false, failOpen: nil}
}

func strDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func newIdempotencyToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "speakeasy-hooks-" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// httpMessage builds the stderr message for a non-2xx transport failure,
// preferring the server's message field.
func httpMessage(res ingestResult) string {
	if msg := strings.TrimSpace(res.decision.Message); msg != "" {
		return msg
	}
	return fmt.Sprintf("Speakeasy hook returned HTTP %d", res.statusCode)
}
