package chat

import (
	"sort"
	"strings"
)

// sourceAliases maps canonical product-surface slugs to raw message-source
// values written by hook, generic-ingest, and compliance-import pipelines.
// Keeping legacy aliases here gives the agent-type filter one value per surface
// while still matching historical sessions.
var sourceAliases = map[string][]string{
	"claude":          {"claude", "claude-desktop", "claude-chat-desktop", "Claude Chat Desktop"},
	"claude-chat-web": {"claude-chat-web", "Claude Chat Web"},
	"claude-code":     {"claude-code", "ClaudeCode"},
	// Claude Code Desktop is its own surface, distinct from the claude-code
	// CLI and from cowork. Note: sessions captured before surface resolution
	// landed stored this adapter slug for cowork sessions too — those
	// historical rows are indistinguishable from genuine CCD by source alone.
	"claude-code-desktop": {"claude-code-desktop"},
	"cowork":              {"cowork", "claude-cowork", "Claude Cowork"},
	"cursor":              {"cursor", "Cursor"},
	"codex":               {"codex", "Codex"},
	// ChatGPT (web/desktop chat + Work mode) — rows arrive via the OpenAI
	// compliance import pipelines under the chatgpt hook source.
	"chatgpt": {"chatgpt", "ChatGPT"},
	// Codex cloud web tasks — transcripts imported from the CODEX_LOG
	// compliance feed, distinct from "codex" (live device sessions captured
	// by hooks/OTEL).
	"codex-web": {"codex-web", "CodexWeb", "Codex Web"},
}

// openclaw is deliberately absent, like opencode: the generated plugin always
// passes --provider=openclaw, so there is only ever one raw spelling and
// pass-through already gives the filter a single entry. An alias folding a
// capitalised variant would be worse than none, because hook_source keeps its
// casing into ClickHouse and the summary MVs match the lowercase value only:
// the filter would show one surface while the usage rows silently undercounted.

// rawToCanonicalSource is the reverse of sourceAliases: each known raw value
// mapped to its canonical form, built once at init.
var rawToCanonicalSource = func() map[string]string {
	m := make(map[string]string)
	for canonical, raws := range sourceAliases {
		for _, raw := range raws {
			m[raw] = canonical
		}
	}
	return m
}()

// CanonicalSource returns the canonical source for a raw message source. Input
// is trimmed; whitespace-only values return "" so callers can drop them. Values
// without a known alias are returned trimmed and unchanged. Exported for the
// risk finding writer, which stamps the canonical slug onto ClickHouse rows so
// the LowCardinality column stays clean and values match the agent-type filter.
func CanonicalSource(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if canonical, ok := rawToCanonicalSource[s]; ok {
		return canonical
	}
	return s
}

// canonicalizeSources maps raw message sources to their canonical form,
// dropping empties and duplicates. The result is sorted so the agent-type
// filter renders a stable, de-duplicated list.
func canonicalizeSources(raws []string) []string {
	seen := make(map[string]struct{}, len(raws))
	out := make([]string, 0, len(raws))
	for _, raw := range raws {
		s := CanonicalSource(raw)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// expandSourceAliases replaces each canonical source with all of its raw
// aliases so the source filter matches sessions recorded under any legacy value
// (e.g. selecting "claude-code" also matches chats stored as "ClaudeCode").
// Values without aliases pass through unchanged. Duplicates are removed while
// preserving order.
func expandSourceAliases(sources []string) []string {
	seen := make(map[string]struct{}, len(sources))
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		aliases, ok := sourceAliases[s]
		if !ok {
			aliases = []string{s}
		}
		for _, a := range aliases {
			if _, dup := seen[a]; dup {
				continue
			}
			seen[a] = struct{}{}
			out = append(out, a)
		}
	}
	return out
}
