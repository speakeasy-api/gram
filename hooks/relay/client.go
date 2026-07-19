package relay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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

const skillUploadBudget = 30 * time.Second

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

type skillCapture struct {
	rawSHA256       string
	contentRequired bool
}

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
	failOpen     *bool
	skillCapture *skillCapture
}

// accepted reports a definitive 2xx exchange — the server stored (or
// deduped) the event. The one classification used by the live path, the
// spool, and the drain; keep them agreeing by never inlining the range.
func (r ingestResult) accepted() bool {
	return r.statusCode >= 200 && r.statusCode < 300
}

// unsent reports whether the control plane failed to store the event: the
// server was unreachable (statusCode 0), failing (5xx), or shedding load
// (429/408 — the request wasn't processed, and replaying later is exactly
// what a rate-limiting server wants). Other 4xx are the server answering —
// a replay would fail identically. Matches the device agent's downtime
// classification (its ADR-0010).
func (r ingestResult) unsent() bool {
	return r.statusCode == 0 || r.statusCode >= 500 ||
		r.statusCode == http.StatusTooManyRequests || r.statusCode == http.StatusRequestTimeout
}

// client posts canonical hook events through the generated ingest SDK with
// bounded retries and a reused idempotency token so redelivered requests are
// stored exactly once.
type client struct {
	sdk *sdk.SpeakeasyHooks
	// budget caps one send end to end; a field so tests can shrink it.
	budget time.Duration
	// replayed stamps X-Gram-Replayed on every request via the typed SDK
	// field, marking a drain redelivery so the server applies the long
	// idempotency window and tags telemetry. Set by newReplayClient.
	replayed bool
}

func newClient(serverURL string) *client {
	return &client{
		budget: sendBudget,
		sdk: sdk.New(
			sdk.WithServerURL(strings.TrimRight(serverURL, "/")),
			sdk.WithClient(&http.Client{
				Timeout: perAttemptTime,
				CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}),
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

func (cl *client) uploadSkillContent(ctx context.Context, c creds, rawSHA256, content string) error {
	ctx, cancel := context.WithTimeout(ctx, skillUploadBudget)
	defer cancel()

	request := operations.UploadSkillContentRequest{
		GramKey:     nil,
		GramProject: nil,
		Body: components.UploadSkillContentPayload{
			Content:       content,
			RawSha256:     rawSHA256,
			SchemaVersion: components.SchemaVersionHookSkillContentV1,
		},
	}
	security := &operations.UploadSkillContentSecurity{
		ApikeyHeaderGramKey:          &c.APIKey,
		ProjectSlugHeaderGramProject: nil,
	}
	if c.Project != "" {
		security.ProjectSlugHeaderGramProject = &c.Project
	}

	var response *operations.UploadSkillContentResponse
	var err error
	for attempt := 0; ; attempt++ {
		response, err = cl.sdk.Hooks.UploadSkillContent(ctx, request, security)
		if err == nil {
			break
		}
		if interpretError(err).statusCode != 0 || attempt >= 2 || ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(attempt+1) * 250 * time.Millisecond):
		}
	}
	if response == nil || response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected skill upload response status")
	}
	return nil
}

// send posts the payload to the ingest endpoint authenticated with c. The
// SDK's built-in retries do not replay connection errors for POSTs, so pure
// transport failures (statusCode 0, the server was never reached) are replayed
// here — safe because the Idempotency-Key is minted once and reused, and
// necessary because a blocking hook would otherwise deny over one dropped
// connection.
//
// The caller mints idemKey (see deliver) so the same key survives beyond
// this exchange: a payload spooled after a failed send replays under the
// original key, and the server dedupes it against any partially delivered
// original.
func (cl *client) send(ctx context.Context, c creds, body components.IngestRequestBody, idemKey string) ingestResult {
	ctx, cancel := context.WithTimeout(ctx, cl.budget)
	defer cancel()

	req := operations.IngestHookEventRequest{
		GramKey:        new(c.APIKey),
		GramProject:    nil,
		IdempotencyKey: new(idemKey),
		XGramReplayed:  nil,
		Body:           body,
	}
	if cl.replayed {
		req.XGramReplayed = new(true)
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

	out := ingestResult{statusCode: res.StatusCode, decision: decision{Decision: "", Reason: "", Message: ""}, authRejected: false, failOpen: nil, skillCapture: nil}
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
		if capture, ok := res.IngestHookResult.Effects["skill_capture"].(map[string]any); ok {
			rawSHA256, hashOK := capture["raw_sha256"].(string)
			contentRequired, requiredOK := capture["content_required"].(bool)
			if hashOK && requiredOK && validRawSHA256(rawSHA256) {
				out.skillCapture = &skillCapture{rawSHA256: rawSHA256, contentRequired: contentRequired}
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
			skillCapture: nil,
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
				skillCapture: nil,
			}
		}
		return ingestResult{
			statusCode:   apiErr.StatusCode,
			decision:     decision{Decision: "", Reason: "", Message: ""},
			authRejected: apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden,
			failOpen:     nil,
			skillCapture: nil,
		}
	}
	return ingestResult{statusCode: 0, decision: decision{Decision: "", Reason: "", Message: ""}, authRejected: false, failOpen: nil, skillCapture: nil}
}

func validRawSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
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
