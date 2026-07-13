package relay

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/speakeasy-api/agenthooks"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// Codex carries no tool_use_id and omits tool_input on PostToolUse, so a
// per-event synthesized id diverges between a request (input hash) and its
// completion (input-less hash), while hashing without input would collide
// same-tool requests. The request id is queued per install+session+turn+tool
// and replayed on the matching completion, mirroring the bash sender. A FIFO
// rather than a single slot because completions run through the async sender
// and can lag behind the next same-tool request.

const codexToolQueueCap = 32

// codexToolStatePath returns the id queue file for one install+session+tool,
// or "" when state cannot be kept — correlation then degrades to the
// input-less fallback id.
func codexToolStatePath(cfg Config, session agenthooks.SessionInfo, toolName string) string {
	stateHome := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(stateHome, "gram", "hooks", "codex-tools")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	install := sanitizeMarker(cfg.ServerURL) + "__" + sanitizeMarker(cfg.ProjectSlug)
	key := sanitizeMarker(session.ID+"|"+session.TurnID) + "__" + sanitizeMarker(toolName)
	return filepath.Join(dir, install+"__"+key+".ids")
}

// pushCodexToolID appends a request id to the queue, bounded so missed
// completions cannot grow it without limit.
func pushCodexToolID(path, id string) {
	if path == "" || id == "" {
		return
	}
	withCodexQueueLock(path, func() {
		lines := readCodexToolQueue(path)
		lines = append(lines, id)
		if len(lines) > codexToolQueueCap {
			lines = lines[len(lines)-codexToolQueueCap:]
		}
		_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
	})
}

// popCodexToolID removes and returns the oldest queued id, or "".
func popCodexToolID(path string) string {
	if path == "" {
		return ""
	}
	var first string
	withCodexQueueLock(path, func() {
		lines := readCodexToolQueue(path)
		if len(lines) == 0 {
			return
		}
		first = lines[0]
		if len(lines) == 1 {
			_ = os.Remove(path)
		} else {
			_ = os.WriteFile(path, []byte(strings.Join(lines[1:], "\n")+"\n"), 0o600)
		}
	})
	return first
}

// withCodexQueueLock serializes the queue's read-modify-write across
// concurrent hook processes: the async completion sender can run alongside
// the next same-tool request's hook. The lock rides a dedicated sibling file
// that is never removed — locking the queue file itself would race its own
// unlink. When locking is unavailable the operation degrades to unlocked,
// the bash sender's posture.
func withCodexQueueLock(path string, fn func()) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		fn()
		return
	}
	defer func() { _ = f.Close() }()
	if err := lockFile(f); err != nil {
		fn()
		return
	}
	defer unlockFile(f)
	fn()
}

func readCodexToolQueue(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// queueCodexToolID remembers the id an allowed codex tool request reported so
// the matching completion can replay it. Callers gate on the verdict: a
// denied request never executes, so its completion never drains the queue and
// a stale entry would attach the next same-tool result to the wrong call.
// PermissionRequest must not enqueue either — it also normalizes to
// tool.requested but has no matching completion.
func (r *Relay) queueCodexToolID(e *agenthooks.ToolPreEvent) {
	if e.Provider != agenthooks.ProviderCodex || !e.Tool.Synthesized {
		return
	}
	pushCodexToolID(codexToolStatePath(r.cfg, e.Session, e.Tool.Name), e.Tool.ID)
}

// correlateCodexToolID replays a remembered request id onto a codex tool
// completion so both sides carry the same identity.
func (r *Relay) correlateCodexToolID(typed any, payload *components.IngestRequestBody) {
	e, ok := typed.(*agenthooks.ToolPostEvent)
	if !ok || e.Provider != agenthooks.ProviderCodex || !e.Tool.Synthesized {
		return
	}
	id := popCodexToolID(codexToolStatePath(r.cfg, e.Session, e.Tool.Name))
	if id == "" {
		return
	}
	if payload.Data != nil && payload.Data.ToolCall != nil {
		payload.Data.ToolCall.ID = &id
	}
}
