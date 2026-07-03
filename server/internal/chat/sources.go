package chat

import (
	"sort"
	"strings"
)

// sourceAliases maps a canonical agent source to every raw message-source value
// that should collapse into it. Legacy chats recorded Claude Code as
// "ClaudeCode" before the hook pipeline standardized on "claude-code"; without
// this mapping the agent-type filter shows both as separate entries (the
// dashboard title-cases "claude-code" into "Claude Code" but leaves the
// delimiter-less "ClaudeCode" untouched). The canonical value is the
// delimited form so the dashboard renders a single, correctly spaced label.
var sourceAliases = map[string][]string{
	"claude-code": {"claude-code", "ClaudeCode"},
}

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

// canonicalSource returns the canonical source for a raw message source. Input
// is trimmed; whitespace-only values return "" so callers can drop them. Values
// without a known alias are returned trimmed and unchanged.
func canonicalSource(raw string) string {
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
		s := canonicalSource(raw)
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
