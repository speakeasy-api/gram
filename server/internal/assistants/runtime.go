package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

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
	Content    string            `json:"content,omitempty"`
	ToolCalls  []runtimeToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

type runtimeToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type runtimeTurnRequest struct {
	Input      string             `json:"input"`
	AuthToken  string             `json:"auth_token,omitempty"`
	MCPServers []runtimeMCPServer `json:"mcp_servers,omitempty"`
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
