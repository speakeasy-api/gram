package assistants

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/chat"
)

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

func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	}
	return false
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
	Input     string `json:"input"`
	AuthToken string `json:"auth_token,omitempty"`
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

// classifyTurnError buckets a /turn error into runtime-unhealthy (tear
// down), completion-failed (terminal), or history-corrupted (self-heal).
// "provider error" is agentkit-provider-openrouter's prefix; "completion
// failed" is Gram's gateway-stamped variant. chat.IsHistoryCorrupted
// detects the Gram-owned marker stamped on upstream 400/422 responses.
func classifyTurnError(err error) error {
	if err == nil {
		return nil
	}
	if chat.IsHistoryCorrupted(err) {
		return ErrHistoryCorrupted
	}
	msg := err.Error()
	if strings.Contains(msg, "provider error") || strings.Contains(msg, "completion failed") {
		return ErrCompletionFailed
	}
	return ErrRuntimeUnhealthy
}
