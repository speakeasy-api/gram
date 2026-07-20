package assistants

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/chat"
)

// runtimeHTTPDoer is the HTTP surface both backends use to reach the in-pod
// runner. Production builds it from the guardian egress policy; tests inject a
// plain client.
type runtimeHTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// defaultRuntimeGuestPort is the HTTP port the runner listens on inside a
// guest VM. Backends use it when wiring service ports on freshly launched
// machines.
const defaultRuntimeGuestPort = 8081

const (
	// runnerDefaultReqTimeout bounds runner HTTP calls that do not carry
	// their own timeout (state reads and other short requests).
	runnerDefaultReqTimeout = 2 * time.Minute
	// runnerHealthProbeTimeoutSeconds caps a single /healthz probe so one
	// stalled probe cannot overshoot a backend's overall health budget.
	runnerHealthProbeTimeoutSeconds = 2
	// runnerMaxResponseBytes bounds runner response reads so a misbehaving
	// runner cannot exhaust server memory.
	runnerMaxResponseBytes = 4 << 20
)

// runtimeResourcePrefix names runner resources across backends (GKE claims,
// local containers and volumes).
const runtimeResourcePrefix = "gram-asst"

// Runner workloads are labelled with assistant/project identity across
// backends (GKE claim labels, local container labels) so owned resources can
// be rediscovered and filtered uniformly.
const (
	runtimeLabelAssistantID          = "gram.speakeasy.com/assistant-id"
	runtimeLabelProjectID            = "gram.speakeasy.com/project-id"
	runtimeLabelRole                 = "gram.speakeasy.com/role"
	runtimeLabelRoleAssistantRuntime = "assistant_runtime"
	runtimeLabelSpecHash             = "gram.speakeasy.com/spec-hash"
)

// runtimeImageRef joins an image repository and tag into the "<repo>[:<tag>]"
// reference form backends launch machines with and compare against.
func runtimeImageRef(image, tag string) string {
	if tag == "" {
		return image
	}
	return image + ":" + tag
}

// runnerClient speaks the runner HTTP protocol for backends that dial the
// runner directly by base URL (GKE pod IPs, local containers). Fly keeps its
// own request layer for machine pinning and response-size limits.
type runnerClient struct {
	do      runtimeHTTPDoer
	backend string
}

func (c runnerClient) request(ctx context.Context, baseURL string, r runtimeHTTPRequest) ([]byte, error) {
	reqCtx, cancel := runtimeRequestContext(ctx, r.MaxTimeSeconds, runnerDefaultReqTimeout)
	defer cancel()

	var reader io.Reader
	if r.Body != nil {
		reader = bytes.NewReader(r.Body)
	}
	req, err := http.NewRequestWithContext(reqCtx, r.Method, baseURL+r.Path, reader)
	if err != nil {
		return nil, fmt.Errorf("build %s runtime request: %w", c.backend, err)
	}
	if r.ContentType != "" {
		req.Header.Set("Content-Type", r.ContentType)
	}
	if r.IdempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", r.IdempotencyKey)
	}

	resp, err := c.do.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute %s runtime request: %w", c.backend, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, runnerMaxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read %s runtime response: %w", c.backend, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &runtimeResponseError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(respBody))}
	}
	return respBody, nil
}

// health polls /healthz until it answers, the timeout elapses, or ctx ends.
// The whole loop runs under a timeout-scoped context so an in-flight probe
// cannot overshoot the budget.
func (c runnerClient) health(ctx context.Context, baseURL string, timeout, poll time.Duration) error {
	healthCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		probe := runtimeHTTPRequest{Method: http.MethodGet, Path: "/healthz", ContentType: "", Body: nil, MaxTimeSeconds: runnerHealthProbeTimeoutSeconds, IdempotencyKey: ""}
		if _, err := c.request(healthCtx, baseURL, probe); err == nil {
			return nil
		}
		select {
		case <-healthCtx.Done():
			if ctx.Err() != nil {
				return fmt.Errorf("wait for %s runtime health: %w", c.backend, ctx.Err())
			}
			return fmt.Errorf("%w: %s runtime health check timed out", ErrRuntimeUnhealthy, c.backend)
		case <-time.After(poll):
		}
	}
}

func (c runnerClient) state(ctx context.Context, baseURL string) (runnerStateResponse, error) {
	body, err := c.request(ctx, baseURL, runtimeHTTPRequest{Method: http.MethodGet, Path: "/state", ContentType: "", Body: nil, MaxTimeSeconds: 0, IdempotencyKey: ""})
	if err != nil {
		return runnerStateResponse{}, err
	}
	var state runnerStateResponse
	if err := json.Unmarshal(body, &state); err != nil {
		return runnerStateResponse{}, fmt.Errorf("decode %s runtime state: %w", c.backend, err)
	}
	return state, nil
}

// turn delivers a turn to /threads/{threadID}/turn, wrapping failures with
// the classifyTurnError sentinel the service dispatches on.
func (c runnerClient) turn(ctx context.Context, baseURL string, runtime assistantRuntimeRecord, threadID uuid.UUID, idempotencyKey, authToken, prompt string, mcpServers []runtimeMCPServer, timeout time.Duration) error {
	reqBody, err := json.Marshal(runtimeTurnRequest{
		Input:       prompt,
		InputParts:  nil,
		AuthToken:   authToken,
		MCPServers:  mcpServers,
		AssistantID: runtime.AssistantID.String(),
		ProjectID:   runtime.ProjectID.String(),
	})
	if err != nil {
		return fmt.Errorf("marshal %s runtime turn request: %w", c.backend, err)
	}
	req := runtimeHTTPRequest{
		Method:         http.MethodPost,
		Path:           "/threads/" + threadID.String() + "/turn",
		ContentType:    "application/json",
		Body:           reqBody,
		MaxTimeSeconds: int(timeout / time.Second),
		IdempotencyKey: idempotencyKey,
	}
	if _, err := c.request(ctx, baseURL, req); err != nil {
		return fmt.Errorf("%w: execute %s turn request: %w", classifyTurnError(err), c.backend, err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// runtimeRequestContext returns a child context bounded by the caller's
// MaxTimeSeconds when set, otherwise by fallback. Caller is responsible for
// invoking the returned cancel.
func runtimeRequestContext(parent context.Context, maxTimeSeconds int, fallback time.Duration) (context.Context, context.CancelFunc) {
	if maxTimeSeconds > 0 {
		return context.WithTimeout(parent, time.Duration(maxTimeSeconds)*time.Second) //nolint:gosec // cancel returned to caller
	}
	return context.WithTimeout(parent, fallback) //nolint:gosec // cancel returned to caller
}

// threadBootstrap is the response the runner receives from
// /rpc/assistants.getThreadBootstrap when a /turn lands for a thread it
// has not seen yet. It carries everything the runtime needs to bring up
// a fresh per-thread agent task — the runner already holds an
// assistant-scoped JWT from the /turn request, so the auth token is not
// included.
type threadBootstrap struct {
	Model          string             `json:"model"`
	Instructions   string             `json:"instructions,omitempty"`
	CompletionsURL string             `json:"completions_url"`
	ChatID         string             `json:"chat_id"`
	MCPServers     []runtimeMCPServer `json:"mcp_servers"`
	History        []runtimeMessage   `json:"history,omitempty"`
	ContextWindow  uint64             `json:"context_window,omitempty"`
	Compaction     CompactionPolicy   `json:"compaction"`
	SourceRefJSON  json.RawMessage    `json:"source_ref_json,omitempty"`
}

type runtimeMCPServer struct {
	ID      string            `json:"id"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// runtimeMessage is the wire shape for a single replayed transcript entry sent
// to the runner. It mirrors the shape the runner expects (see agents/runner's
// RunnerMessage) which in turn expands to one agentkit Item per message. Keep
// fields in sync with that struct.
type runtimeMessage struct {
	Role       string            `json:"role"`
	Content    runtimeContent    `json:"content,omitzero"`
	ToolCalls  []runtimeToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

// runtimeContent is the string-or-parts union carried in a message's content
// slot, mirroring the runner's RunnerContent and the OpenRouter content union.
// Text-only content marshals as a bare JSON string so runners (and servers)
// that predate structured parts keep interoperating; both encodings decode.
type runtimeContent struct {
	// Str is the plain-text content used when Parts is nil.
	Str string
	// Parts is the structured content; a non-nil slice wins over Str.
	Parts []runtimeContentPart
}

const (
	contentPartTypeText     = "text"
	contentPartTypeImageURL = "image_url"
)

// runtimeContentPart is one element of a structured content array. Exactly
// the field matching Type is set; unknown types decode with only Type
// populated so callers can reject them.
type runtimeContentPart struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	ImageURL *runtimeImageURL `json:"image_url,omitempty"`
}

type runtimeImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

func runtimeTextContent(text string) runtimeContent {
	return runtimeContent{Str: text, Parts: nil}
}

// IsZero reports whether the content is empty, letting `omitzero` drop the
// content key exactly where the previous plain-string field's `omitempty` did.
func (c runtimeContent) IsZero() bool {
	return c.Str == "" && c.Parts == nil
}

// Text returns the plain-text projection: the bare string, or the text parts
// joined with newlines. Image parts contribute nothing.
func (c runtimeContent) Text() string {
	if c.Parts == nil {
		return c.Str
	}
	var sb strings.Builder
	for _, part := range c.Parts {
		if part.Type != contentPartTypeText {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(part.Text)
	}
	return sb.String()
}

// supportedParts reports whether every part is a shape the runner wire
// protocol models (text, or image_url with a URL). Content decoded from
// persisted OpenRouter JSON may carry other part types; callers fall back to
// the plain-text projection rather than forward parts the runner would reject.
func (c runtimeContent) supportedParts() bool {
	for _, part := range c.Parts {
		switch part.Type {
		case contentPartTypeText:
		case contentPartTypeImageURL:
			if part.ImageURL == nil || part.ImageURL.URL == "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (c runtimeContent) MarshalJSON() ([]byte, error) {
	if c.Parts != nil {
		out, err := json.Marshal(c.Parts)
		if err != nil {
			return nil, fmt.Errorf("marshal content parts: %w", err)
		}
		return out, nil
	}
	out, err := json.Marshal(c.Str)
	if err != nil {
		return nil, fmt.Errorf("marshal content string: %w", err)
	}
	return out, nil
}

func (c *runtimeContent) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	switch {
	case len(trimmed) > 0 && trimmed[0] == '[':
		var parts []runtimeContentPart
		if err := json.Unmarshal(data, &parts); err != nil {
			return fmt.Errorf("unmarshal content parts: %w", err)
		}
		*c = runtimeContent{Str: "", Parts: parts}
	case bytes.Equal(trimmed, []byte("null")):
		*c = runtimeContent{Str: "", Parts: nil}
	default:
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("unmarshal content string: %w", err)
		}
		*c = runtimeContent{Str: s, Parts: nil}
	}
	return nil
}

type runtimeToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type runtimeTurnRequest struct {
	Input string `json:"input"`
	// InputParts optionally carries structured content parts alongside Input.
	// The runner appends them after the text when building the user item, so
	// a turn can attach images without widening the string-typed Input (and
	// the RunTurn plumbing behind it). Unused until the server produces image
	// parts; kept optional so older runners ignore it.
	InputParts []runtimeContentPart `json:"input_parts,omitempty"`
	AuthToken  string               `json:"auth_token,omitempty"`
	MCPServers []runtimeMCPServer   `json:"mcp_servers,omitempty"`
	// AssistantID lets a runner that booted without GRAM_ASSISTANT_ID env
	// (e.g. a generic warm-pool sandbox on GKE) learn which assistant it serves
	// from the turn. Boot env wins when present, so it is harmless and unused
	// on Fly.
	AssistantID string `json:"assistant_id,omitempty"`
	// ProjectID travels with AssistantID under the same set-once discipline.
	// The runner stamps it on exported trace spans so traces filter per
	// project; it never drives runner behavior.
	ProjectID string `json:"project_id,omitempty"`
}

type runtimeHTTPRequest struct {
	Method      string
	Path        string
	ContentType string
	Body        []byte
	// MaxTimeSeconds overrides the default per-request HTTP timeout. 0 uses
	// a conservative default appropriate for health checks; long turn
	// requests should set this large enough to cover the full agent loop
	// (tool calls + LLM turns).
	MaxTimeSeconds int
	// IdempotencyKey sets X-Idempotency-Key; the runner skips re-running a
	// turn it has already processed under this key. Use the event DB id so
	// the same key flows through workflow retries, coordinator re-signals,
	// and reaper requeues.
	IdempotencyKey string
}

// ErrRuntimeUnhealthy signals that a turn failed because the runtime itself
// is unreachable or has exited (connection refused, context cancel from a
// closed done channel, missing state). Callers treat this as "tear down and
// re-admit" rather than "retry the event inline", which would otherwise
// hammer a dead VM with duplicate deliveries.
var ErrRuntimeUnhealthy = errors.New("assistant runtime unhealthy")

// ErrCompletionFailed signals that a turn failed because the upstream
// completion provider (OpenRouter/Anthropic/etc) refused the request or
// returned a non-retryable error. The runtime itself is healthy — replaying
// the same input would just produce the same failure, so callers terminally
// fail the event and leave the VM warm to handle subsequent events.
var ErrCompletionFailed = errors.New("assistant completion failed")

// ErrHistoryCorrupted signals the upstream provider rejected the replayed
// transcript as malformed or oversize. Distinct from ErrCompletionFailed:
// trimming history and retrying typically clears it, so callers self-heal
// once per event before giving up.
var ErrHistoryCorrupted = errors.New("assistant chat history corrupted")

// runtimeResponseError is returned by a backend's HTTP layer when the runner
// answered with a non-2xx status. Its presence in an error chain is proof the
// VM is alive — the failure is the runner's response, not the transport — so
// classifyTurnError never buckets it as runtime-unhealthy on its own.
type runtimeResponseError struct {
	StatusCode int
	Body       string
}

func (e *runtimeResponseError) Error() string {
	return fmt.Sprintf("status=%d body=%s", e.StatusCode, e.Body)
}

// deterministicClientError reports whether the status is a client error that
// replaying would just reproduce. 408/429 are excluded — they are transient
// and worth retrying.
func (e *runtimeResponseError) deterministicClientError() bool {
	if e.StatusCode < 400 || e.StatusCode >= 500 {
		return false
	}
	return e.StatusCode != http.StatusRequestTimeout && e.StatusCode != http.StatusTooManyRequests
}

// runnerHistoryRejectMarkers are substrings the runner stamps when it refuses
// to replay a malformed transcript (normalize_history). Routing these to
// ErrHistoryCorrupted lets the self-heal trim the bad tail instead of tearing
// the VM down and replaying the same poison forever.
var runnerHistoryRejectMarkers = []string{
	"decode tool_call arguments",
	"missing tool_call_id",
}

// classifyTurnError buckets a /turn error into runtime-unhealthy (tear down),
// completion-failed (terminal), or history-corrupted (self-heal). The guiding
// rule is the failure *layer*: if the runner answered (a runtimeResponseError
// is in the chain) the VM is alive and must not be torn down on its own; only
// a transport failure — or a 5xx we cannot attribute to a deterministic cause —
// is treated as unhealthy. "provider error" is agentkit-provider-openrouter's
// prefix; "completion failed" is Gram's gateway-stamped variant;
// chat.IsHistoryCorrupted detects the Gram marker stamped on upstream 400/422.
func classifyTurnError(err error) error {
	if err == nil {
		return nil
	}
	if chat.IsHistoryCorrupted(err) {
		return ErrHistoryCorrupted
	}
	msg := err.Error()
	for _, marker := range runnerHistoryRejectMarkers {
		if strings.Contains(msg, marker) {
			return ErrHistoryCorrupted
		}
	}
	if strings.Contains(msg, "provider error") || strings.Contains(msg, "completion failed") {
		return ErrCompletionFailed
	}
	// The runner answered with a deterministic client error (4xx, excluding
	// transient 408/429): replaying repeats it, so fail terminally rather than
	// churning the VM.
	var respErr *runtimeResponseError
	if errors.As(err, &respErr) && respErr.deterministicClientError() {
		return ErrCompletionFailed
	}
	// Transport failure, or a 5xx we cannot attribute to a deterministic cause.
	// Tear the runtime down and re-admit under a fresh VM — but bounded by
	// maxRuntimeTeardowns so a misclassified deterministic error cannot loop.
	return ErrRuntimeUnhealthy
}

// turnOutcome is a low-cardinality classifyTurnError bucket reported as a
// metric dimension. The values are alert/dashboard contract — keep them stable.
type turnOutcome string

const (
	turnOutcomeRuntimeUnhealthy          turnOutcome = "runtime_unhealthy"
	turnOutcomeRuntimeUnhealthyExhausted turnOutcome = "runtime_unhealthy_exhausted"
	turnOutcomeHistoryCorrupted          turnOutcome = "history_corrupted"
	turnOutcomeCompletionFailed          turnOutcome = "completion_failed"
	turnOutcomeTransient                 turnOutcome = "transient"
)

// turnErrorBucket names the classifyTurnError outcome for metrics. transient
// covers anything that falls through to the capped inline-retry path.
func turnErrorBucket(err error) turnOutcome {
	switch {
	case errors.Is(err, ErrRuntimeUnhealthy):
		return turnOutcomeRuntimeUnhealthy
	case errors.Is(err, ErrHistoryCorrupted):
		return turnOutcomeHistoryCorrupted
	case errors.Is(err, ErrCompletionFailed):
		return turnOutcomeCompletionFailed
	default:
		return turnOutcomeTransient
	}
}
