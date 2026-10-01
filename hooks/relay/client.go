package relay

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	sdk "github.com/speakeasy-api/gram/hooks/sdk"
	"github.com/speakeasy-api/gram/hooks/sdk/models/apierrors"
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
	"github.com/speakeasy-api/gram/hooks/sdk/models/operations"
	"github.com/speakeasy-api/gram/hooks/sdk/retry"
	"github.com/speakeasy-api/gram/hooks/wire"
)

const perAttemptTime = 10 * time.Second

// sendBudget bounds one send end to end — the SDK's internal 30s retry budget
// and the transport replays below stack, and a gating hook that outlives the
// provider's 60s timeout fails closed uncontrolled instead of returning a
// verdict.
const sendBudget = 45 * time.Second

// gateSendBudget is the floor for the synchronous verdict exchange, and the
// whole budget when the context carries no deadline. The chain it was sized
// for is 5s network < the openclaw daemon's ~9s gate deadline < the shim's
// 10s wall: a fail-closed block must land before an upstream deadline expires
// and dissolves it into an allow. Observes keep the full sendBudget.
const gateSendBudget = 5 * time.Second

// maxGateSendBudget caps the budget gateBudget derives: past this the user is
// waiting on every tool call for a control plane that is not answering.
const maxGateSendBudget = 15 * time.Second

// gateBudget sizes the verdict exchange against the wall the provider actually
// imposes rather than the tightest wall any provider imposes. agenthooks
// already puts that wall on the handler context (90% of the --timeout baked
// into the generated config, else its own 55s default), so half of what is
// left adapts where a per-provider constant would drift. No deadline means the
// Pi/OpenClaw shim path, where the daemon enforces its own: keep the floor.
func gateBudget(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return gateSendBudget
	}
	remaining := time.Until(deadline)
	budget := max(min(remaining/2, maxGateSendBudget), gateSendBudget)
	// The floor must never outrun the caller's own wall, or the verdict lands
	// on the wrong side of it.
	return min(budget, remaining)
}

// sendAttempts is how many HTTP attempts one send makes before giving up.
const sendAttempts = 3

// minAttemptTime keeps the last attempt of a nearly spent budget from starting
// with a deadline that has already passed.
const minAttemptTime = time.Second

// connectTimeout bounds the phases that stall when the network path is broken
// rather than merely slow: DNS plus TCP connect, and the TLS handshake.
// Bounding these — instead of slicing the budget across attempts — is what
// keeps the two cases apart: a black-holed connection fails here and leaves
// budget for a replay, while a control plane that is simply slow to answer
// still gets the rest of the budget to answer in. Every hook is a fresh
// process, so this handshake is paid per event and is what an intercepting
// proxy slows down (DNO-1232).
const connectTimeout = 2 * time.Second

// attemptTimeout bounds one HTTP attempt by the transport ceiling and by
// whatever is left of the budget it runs under.
func attemptTimeout(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return perAttemptTime
	}
	remaining := time.Until(deadline)
	if remaining <= minAttemptTime {
		return minAttemptTime
	}
	return min(remaining, perAttemptTime)
}

// ingestTransport bounds connect and handshake on top of the default
// transport's pooling and proxy handling, then wraps it in the device headers.
func ingestTransport() http.RoundTripper {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &deviceTransport{base: http.DefaultTransport}
	}
	transport := base.Clone()
	transport.DialContext = (&net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = connectTimeout
	return &deviceTransport{base: transport}
}

// maxCauseDetail bounds the diagnostic written to the debug log. The text
// comes from the transport, and the SDK's APIError stringifies the whole
// response body — an intermediary's error page would otherwise land, in full,
// in a file people paste into support threads.
const maxCauseDetail = 256

// urlInError matches the request URL the transport quotes in its errors, so
// redactURL can mask any credential-bearing parameters it carries.
var urlInError = regexp.MustCompile(`https?://[^\s"]+`)

// sanitizeCauseDetail bounds a transport error's text and masks secrets in any
// URL it quotes, before it reaches the debug log.
func sanitizeCauseDetail(detail string) string {
	detail = strings.TrimSpace(urlInError.ReplaceAllStringFunc(detail, redactURL))
	if len(detail) > maxCauseDetail {
		// ToValidUTF8 drops the rune the cut may have split.
		detail = strings.ToValidUTF8(detail[:maxCauseDetail], "") + "…"
	}
	return detail
}

// failureCause names why an exchange produced no usable status. They all
// report statusCode 0, so without this an unresolvable host, an intercepting
// proxy, and an expired budget are one indistinguishable "HTTP 0".
type failureCause string

const (
	// causeNone is a definitive exchange: the status carries the meaning.
	causeNone failureCause = ""
	causeDNS  failureCause = "dns"
	// causeTLS is a handshake or certificate rejection — typically an
	// intercepting proxy on a managed machine.
	causeTLS      failureCause = "tls"
	causeTimeout  failureCause = "timeout"
	causeCanceled failureCause = "canceled"
	// causeConnection is a refused, reset, or unroutable connection.
	causeConnection failureCause = "connection"
	// causeUnreadable is a response that arrived but could not be read as a
	// verdict — an intermediary's JSON, a captive portal, a skewed server.
	causeUnreadable failureCause = "unreadable-response"
	causeUnknown    failureCause = "unknown"
)

// classifyTransportError names the failure behind an error that produced no
// HTTP response. Most specific first: a DNS lookup that timed out reports as
// DNS, because that is the thing to go and fix.
func classifyTransportError(err error) failureCause {
	if err == nil {
		return causeNone
	}

	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return causeDNS
	}

	var certErr *tls.CertificateVerificationError
	var recordErr tls.RecordHeaderError
	var hostErr x509.HostnameError
	var authorityErr x509.UnknownAuthorityError
	if errors.As(err, &certErr) || errors.As(err, &recordErr) ||
		errors.As(err, &hostErr) || errors.As(err, &authorityErr) {
		return causeTLS
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return causeTimeout
	}
	if errors.Is(err, context.Canceled) {
		return causeCanceled
	}
	// http.Client.Timeout and the net package report their own timeouts
	// through this interface rather than the context sentinels.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return causeTimeout
	}

	// Refused, reset, and unroutable all surface as an OpError, and share one
	// cause because they share one remedy: look at what sits between the
	// machine and the control plane.
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return causeConnection
	}

	return causeUnknown
}

// skillUploadBudget bounds the content upload that runs inline after a
// delivered event. The upload is best-effort — the server re-requests the
// content on the skill's next activation — so it gets a budget that keeps
// sendBudget plus this stall under the provider's 60s hook timeout.
const skillUploadBudget = 10 * time.Second

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
	// blockEffect carries the structured requestable-block metadata from the
	// response's "block" effects; nil when the server sent none.
	blockEffect *wire.BlockEffect
	// cause names why statusCode is 0. Empty for any definitive exchange.
	cause failureCause
	// causeDetail is the underlying error text. Debug log only — it carries
	// the server URL and transport internals, which belong in a diagnostic
	// rather than in a blocked tool call.
	causeDetail string
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
				Timeout:   perAttemptTime,
				Transport: ingestTransport(),
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
			SchemaVersion: components.UploadSkillContentPayloadSchemaVersionHookSkillContentV1,
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
		attemptCtx, cancelAttempt := context.WithTimeout(ctx, attemptTimeout(ctx))
		response, err = cl.sdk.Hooks.UploadSkillContent(attemptCtx, request, security)
		cancelAttempt()
		if err == nil {
			break
		}
		if interpretError(err).statusCode != 0 || attempt >= sendAttempts-1 || ctx.Err() != nil {
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
		// Bounded per attempt so a stalled connection costs one attempt
		// rather than the whole send.
		attemptCtx, cancelAttempt := context.WithTimeout(ctx, attemptTimeout(ctx))
		res, err = cl.sdk.Hooks.Ingest(attemptCtx, req)
		cancelAttempt()
		if err == nil {
			break
		}
		out := interpretError(err)
		if out.statusCode != 0 || attempt >= sendAttempts-1 || ctx.Err() != nil {
			return out
		}
		select {
		case <-ctx.Done():
			return out
		case <-time.After(time.Duration(attempt+1) * 250 * time.Millisecond):
		}
	}

	out := ingestResult{statusCode: res.StatusCode, decision: decision{Decision: "", Reason: "", Message: ""}, authRejected: false, failOpen: nil, skillCapture: nil, blockEffect: nil, cause: causeNone, causeDetail: ""}
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
		out.blockEffect = decodeBlockEffect(res.IngestHookResult.Effects["block"])
	}
	return out
}

// interpretError maps an SDK error onto the relay's result semantics: a typed
// or generic API error carries a definitive status the ratchet can act on; a
// transport failure that never produced a response leaves statusCode 0 plus
// the cause explaining it.
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
			blockEffect:  nil,
			cause:        causeNone,
			causeDetail:  "",
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
			// No decision message: httpMessage then renders the cause, so this
			// failure carries the same slug as every other status-0 outcome.
			// The detail omits err.Error() deliberately — it embeds the whole
			// unparseable body, which is exactly the untrusted content that
			// must not reach the debug log.
			return ingestResult{
				statusCode:   0,
				decision:     decision{Decision: "", Reason: "", Message: ""},
				authRejected: false,
				failOpen:     nil,
				skillCapture: nil,
				blockEffect:  nil,
				cause:        causeUnreadable,
				causeDetail:  fmt.Sprintf("unparseable %d response", apiErr.StatusCode),
			}
		}
		return ingestResult{
			statusCode:   apiErr.StatusCode,
			decision:     decision{Decision: "", Reason: "", Message: ""},
			authRejected: apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden,
			failOpen:     nil,
			skillCapture: nil,
			blockEffect:  nil,
			cause:        causeNone,
			causeDetail:  "",
		}
	}
	detail := ""
	if err != nil {
		detail = sanitizeCauseDetail(err.Error())
	}
	return ingestResult{statusCode: 0, decision: decision{Decision: "", Reason: "", Message: ""}, authRejected: false, failOpen: nil, skillCapture: nil, blockEffect: nil, cause: classifyTransportError(err), causeDetail: detail}
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
// preferring the server's message field. With no status at all the cause
// carries the meaning — "HTTP 0" told the user nothing they could act on.
func httpMessage(res ingestResult) string {
	if msg := strings.TrimSpace(res.decision.Message); msg != "" {
		return msg
	}
	if res.statusCode == 0 {
		return causeMessage(res.cause)
	}
	return fmt.Sprintf("Speakeasy hook returned HTTP %d", res.statusCode)
}

// causeMessage renders a transport failure for the person whose tool call was
// just blocked. The slug is appended so a support thread can quote it.
func causeMessage(cause failureCause) string {
	switch cause {
	case causeDNS:
		return "Speakeasy hooks could not resolve the Speakeasy AI Control Plane host (dns)."
	case causeTLS:
		return "Speakeasy hooks could not establish a secure connection to the Speakeasy AI Control Plane; a TLS-inspecting proxy may be intercepting it (tls)."
	case causeTimeout:
		return "Speakeasy hooks timed out reaching the Speakeasy AI Control Plane (timeout)."
	case causeCanceled:
		return "Speakeasy hooks were interrupted before the Speakeasy AI Control Plane answered (canceled)."
	case causeConnection:
		return "Speakeasy hooks could not connect to the Speakeasy AI Control Plane (connection)."
	case causeUnreadable:
		return "Speakeasy hooks could not read the server's verdict (unreadable-response)."
	default:
		return "Speakeasy hooks could not reach the Speakeasy AI Control Plane (unknown)."
	}
}
