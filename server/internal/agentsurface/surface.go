// Package agentsurface folds the client-supplied hook_source values into the
// fixed set of consuming surfaces that coverage reporting is organized around.
//
// hook_source is not a controlled vocabulary. The Claude hook path stamps a
// flat "claude" for every Claude product, the generic ingest path passes
// payload.Source.Adapter through verbatim, and adapters spell the same product
// several ways ("claude-code", "claudecode", "claude_code"). Folding therefore
// has to happen somewhere, and it belongs next to ingest rather than in each
// consumer: the dashboard previously carried its own copy of this map and
// silently dropped every source missing from it, so an unrecognized adapter
// vanished from the coverage matrix instead of being reported as unmapped.
package agentsurface

import "strings"

// Surface is a consuming surface in the coverage matrix. The zero value is
// SurfaceUnknown, which callers must treat as "not attributable to a surface"
// rather than folding it into SurfaceOther: Other is a real bucket of known
// third-party agents, while Unknown means the fold did not recognize the
// value at all and the operator should be told.
type Surface string

const (
	SurfaceUnknown    Surface = ""
	SurfaceClaudeCode Surface = "claude_code"
	SurfaceClaudeChat Surface = "claude_chat"
	SurfaceCowork     Surface = "cowork"
	SurfaceCodex      Surface = "codex"
	SurfaceCursor     Surface = "cursor"
	SurfaceOther      Surface = "other"
)

// All lists the surfaces in the order the coverage matrix renders them.
// SurfaceUnknown is deliberately absent: it is a reporting signal, not a
// column.
var All = []Surface{
	SurfaceClaudeCode,
	SurfaceClaudeChat,
	SurfaceCowork,
	SurfaceCodex,
	SurfaceCursor,
	SurfaceOther,
}

// bySource maps a normalized hook_source to its surface. Bare "claude" is
// absent on purpose: the Claude hook path stamps it for Claude Code, Cowork
// and Claude Chat alike, so resolving it to any one of them would invent
// attribution. ForHookSource handles it through the agent-variant hint.
var bySource = map[string]Surface{
	"claude-code":         SurfaceClaudeCode,
	"claude-code-cli":     SurfaceClaudeCode,
	"claude-code-web":     SurfaceClaudeCode,
	"claude-code-desktop": SurfaceClaudeCode,
	// claudeSessionSurface stamps "claude-tag" for Claude-in-Slack sessions,
	// which run the Claude Code runtime.
	"claude-tag": SurfaceClaudeCode,

	"claude-chat":     SurfaceClaudeChat,
	"claude-chat-web": SurfaceClaudeChat,
	"claude-desktop":  SurfaceClaudeChat,
	"claude-web":      SurfaceClaudeChat,

	"cowork":         SurfaceCowork,
	"claude-cowork":  SurfaceCowork,
	"cowork-desktop": SurfaceCowork,

	"codex":        SurfaceCodex,
	"codex-cli":    SurfaceCodex,
	"codex-web":    SurfaceCodex,
	"chatgpt":      SurfaceCodex,
	"chatgpt-work": SurfaceCodex,

	"cursor":     SurfaceCursor,
	"cursor-cli": SurfaceCursor,

	"opencode":       SurfaceOther,
	"gemini":         SurfaceOther,
	"gemini-cli":     SurfaceOther,
	"copilot":        SurfaceOther,
	"github-copilot": SurfaceOther,
	"openclaw":       SurfaceOther,
	"litellm":        SurfaceOther,
	"glean":          SurfaceOther,
	"bedrock":        SurfaceOther,
	"aws-bedrock":    SurfaceOther,
	"pi":             SurfaceOther,
}

// compact indexes the same surfaces under their separator-free spelling, so
// "claudecode" resolves alongside "claude-code" without every adapter's
// spelling having to be enumerated. Built once; collisions cannot occur
// because no two bySource keys differ only by their separators.
var compact = func() map[string]Surface {
	index := make(map[string]Surface, len(bySource))
	for source, surface := range bySource {
		index[strings.ReplaceAll(source, "-", "")] = surface
	}
	return index
}()

// Normalize canonicalizes a raw hook_source for lookup: case-folded, trimmed,
// and with underscores and whitespace unified to the hyphen the map keys use.
func Normalize(raw string) string {
	lowered := strings.ToLower(strings.TrimSpace(raw))
	return strings.Join(strings.FieldsFunc(lowered, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '_' || r == '-'
	}), "-")
}

// ForHookSource folds a hook_source into a surface. variant is the session's
// agent variant when the caller has one (the hooks session cache records
// "cowork" or "claude-code" to separate Cowork from Claude Code Desktop);
// pass "" when it is unknown.
//
// The second return reports whether the value was recognized. A false result
// means the source is genuinely unmapped, and callers should surface that as
// unmapped activity rather than discarding the row.
func ForHookSource(source string, variant string) (Surface, bool) {
	normalized := Normalize(source)
	if normalized == "" {
		return SurfaceUnknown, false
	}
	if surface, ok := bySource[normalized]; ok {
		return surface, true
	}
	if surface, ok := compact[strings.ReplaceAll(normalized, "-", "")]; ok {
		return surface, true
	}
	// Bare "claude" carries no product of its own, so the variant decides.
	// Without one the event is real but unattributable, which is a different
	// statement from "we have never heard of this source".
	if normalized == "claude" {
		if surface, ok := bySource[Normalize(variant)]; ok {
			return surface, true
		}
		return SurfaceUnknown, true
	}
	return SurfaceUnknown, false
}
