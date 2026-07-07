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
	lines := readCodexToolQueue(path)
	lines = append(lines, id)
	if len(lines) > codexToolQueueCap {
		lines = lines[len(lines)-codexToolQueueCap:]
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

// popCodexToolID removes and returns the oldest queued id, or "".
func popCodexToolID(path string) string {
	if path == "" {
		return ""
	}
	lines := readCodexToolQueue(path)
	if len(lines) == 0 {
		return ""
	}
	if len(lines) == 1 {
		_ = os.Remove(path)
	} else {
		_ = os.WriteFile(path, []byte(strings.Join(lines[1:], "\n")+"\n"), 0o600)
	}
	return lines[0]
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

// correlateCodexToolID queues the id a codex tool request reports and replays
// it onto the matching completion so both sides carry the same identity. Only
// ToolPreEvent enqueues: PermissionRequest also normalizes to tool.requested
// but has no matching completion, so queueing it would leave a stale id for
// the next completion to pop.
func (r *Relay) correlateCodexToolID(typed any, payload *components.IngestRequestBody) {
	base := agenthooks.EventOf(typed)
	if base == nil || base.Provider != agenthooks.ProviderCodex {
		return
	}
	switch e := typed.(type) {
	case *agenthooks.ToolPreEvent:
		if e.Tool.Synthesized {
			pushCodexToolID(codexToolStatePath(r.cfg, base.Session, e.Tool.Name), e.Tool.ID)
		}
	case *agenthooks.ToolPostEvent:
		if !e.Tool.Synthesized {
			return
		}
		id := popCodexToolID(codexToolStatePath(r.cfg, base.Session, e.Tool.Name))
		if id == "" {
			return
		}
		if payload.Data != nil && payload.Data.ToolCall != nil {
			payload.Data.ToolCall.ID = &id
		}
	}
}
