package relay

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	ingestPath     = "/rpc/hooks.ingest"
	apiKeyHeader   = "Gram-Key"
	projectHeader  = "Gram-Project"
	defaultMaxTry  = 4
	defaultBackoff = time.Second
	perAttemptTime = 10 * time.Second
)

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
}

// client posts canonical hook events to the Gram ingest endpoint with bounded
// retries and a reused idempotency token so redelivered requests are stored
// exactly once.
type client struct {
	http       *http.Client
	maxAttempt int
	backoff    time.Duration
}

func newClient() *client {
	return &client{
		http:       &http.Client{Timeout: perAttemptTime},
		maxAttempt: defaultMaxTry,
		backoff:    defaultBackoff,
	}
}

// send posts the payload to /rpc/hooks.ingest authenticated with c. It retries
// transient failures (connection resets, 5xx) reusing one idempotency token.
func (cl *client) send(ctx context.Context, serverURL string, c creds, payload ingestPayload) ingestResult {
	token := newIdempotencyToken()
	payload.IdempotencyKey = token
	body, err := json.Marshal(payload)
	if err != nil {
		return ingestResult{statusCode: 0, decision: decision{}, authRejected: false}
	}

	url := strings.TrimRight(serverURL, "/") + ingestPath
	var lastStatus int
	for attempt := 1; attempt <= cl.maxAttempt; attempt++ {
		status, respBody, reqErr := cl.attempt(ctx, url, token, c, body)
		if reqErr == nil {
			if status < 500 {
				return interpret(status, respBody)
			}
			lastStatus = status
		}
		if attempt < cl.maxAttempt {
			select {
			case <-ctx.Done():
				return ingestResult{statusCode: lastStatus, decision: decision{}, authRejected: false}
			case <-time.After(cl.backoff * time.Duration(attempt)):
			}
		}
	}
	// Exhausted retries: a persistent 5xx is still a definitive status the
	// caller can act on; a transport error leaves statusCode 0.
	if lastStatus != 0 {
		return interpret(lastStatus, nil)
	}
	return ingestResult{statusCode: 0, decision: decision{}, authRejected: false}
}

func (cl *client) attempt(ctx context.Context, url, token string, c creds, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", token)
	req.Header.Set(apiKeyHeader, c.APIKey)
	if c.Project != "" {
		req.Header.Set(projectHeader, c.Project)
	}

	resp, err := cl.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, respBody, nil
}

func interpret(status int, body []byte) ingestResult {
	res := ingestResult{statusCode: status, decision: decision{}, authRejected: status == http.StatusUnauthorized || status == http.StatusForbidden}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &res.decision)
	}
	return res
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
