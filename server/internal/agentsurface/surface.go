// Package agentsurface folds hook_source values into the consuming surfaces
// coverage reporting is organized around.
//
// hook_source is not a controlled vocabulary: adapters report it verbatim and
// spell the same product several ways.
package agentsurface

import "strings"

// Surface is a consuming surface in the coverage matrix. Unknown means the
// fold did not recognize the value and is not the same as Other, which is a
// real bucket of known third-party agents.
type Surface string

const (
	SurfaceUnknown    Surface = ""
	SurfaceClaudeCode Surface = "claude_code"
	SurfaceClaudeChat Surface = "claude_chat"
	SurfaceCowork     Surface = "cowork"
	SurfaceCodex      Surface = "codex"
	SurfaceCursor     Surface = "cursor"
	SurfaceOther      Surface = "other"

	// SurfaceMCPGateway is Gram's own MCP gateway: hosted MCP servers and
	// gateway endpoints agents connect to directly. It is deliberately absent
	// from All and from the fold below — no hook_source ever names it, because
	// its traffic reaches Gram as MCP requests rather than as agent-side hook
	// reports. Its evidence is assembled from gateway telemetry instead.
	SurfaceMCPGateway Surface = "mcp_gateway"
)

// All lists the agent surfaces hook_source folds onto, in the order the matrix
// renders them. Unknown is a reporting signal, not a column, and the gateway
// is not an agent surface.
var All = []Surface{
	SurfaceClaudeCode,
	SurfaceClaudeChat,
	SurfaceCowork,
	SurfaceCodex,
	SurfaceCursor,
	SurfaceOther,
}

// Columns lists every matrix column in render order. The gateway leads because
// it is the sanctioned path traffic is meant to take; the agent surfaces that
// follow are observed wherever the agent happens to run.
var Columns = append([]Surface{SurfaceMCPGateway}, All...)

// Bare "claude" is absent on purpose: the Claude hook path stamps it for
// Claude Code, Cowork and Claude Chat alike, so mapping it would invent
// attribution. ForHookSource resolves it from the agent variant instead.
var bySource = map[string]Surface{
	"claude-code":         SurfaceClaudeCode,
	"claude-code-cli":     SurfaceClaudeCode,
	"claude-code-web":     SurfaceClaudeCode,
	"claude-code-desktop": SurfaceClaudeCode,
	// Claude-in-Slack, which runs the Claude Code runtime.
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

// compact indexes the same surfaces separator-free, so "claudecode" resolves
// alongside "claude-code" without enumerating every spelling.
var compact = func() map[string]Surface {
	index := make(map[string]Surface, len(bySource))
	for source, surface := range bySource {
		index[strings.ReplaceAll(source, "-", "")] = surface
	}
	return index
}()

// Normalize case-folds a raw hook_source and unifies separators to hyphens.
func Normalize(raw string) string {
	lowered := strings.ToLower(strings.TrimSpace(raw))
	return strings.Join(strings.FieldsFunc(lowered, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '_' || r == '-'
	}), "-")
}

// ForHookSource folds a hook_source into a surface. variant is the session's
// cached agent variant, or "" when unknown.
//
// The second return reports whether the source was recognized. Callers must
// surface an unrecognized source as unmapped rather than discarding the row.
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
	// Recognized but unattributable without a variant, which differs from an
	// unknown source.
	if normalized == "claude" {
		if surface, ok := bySource[Normalize(variant)]; ok {
			return surface, true
		}
		return SurfaceUnknown, true
	}
	return SurfaceUnknown, false
}
