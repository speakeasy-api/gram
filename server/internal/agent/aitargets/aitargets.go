// Package aitargets is the code-owned catalog of AI developer tools the
// device agent's AI scan can report (Shadow AI detection). The catalog is
// server-side only: agents compile their own scan target lists into the
// binary, so this package's consumers are the ingest and read paths — the
// read API decorates display names and categories from it — plus the demo
// seed and dashboard. Target ids align with the dashboard's
// PRODUCT_SURFACE_LABELS keys in client/dashboard/src/lib/formatPlatform.ts
// where a surface already exists there, and with the ids the device agent's
// compiled-in list reports.
package aitargets

import "sync"

// Category classifies what kind of AI tool a target is.
type Category string

const (
	// CategoryHarness is an agentic coding tool or AI IDE (e.g. Claude Code,
	// Cursor).
	CategoryHarness Category = "harness"

	// CategoryLocalModel is a local model runtime (e.g. Ollama, LM Studio).
	CategoryLocalModel Category = "local_model"
)

// Target is one AI tool the catalog knows.
type Target struct {
	// ID is the stable catalog identifier scan reports and reads key on.
	// Never reuse an id for a different tool.
	ID string

	// DisplayName is the human-readable name surfaced in the dashboard.
	DisplayName string

	// Category classifies the target.
	Category Category
}

// targets is the catalog. Keep ids aligned with the dashboard's
// PRODUCT_SURFACE_LABELS keys where the surface exists there and with the
// device agent's compiled-in scan target list.
var targets = []Target{
	{ID: "claude-code", DisplayName: "Claude Code", Category: CategoryHarness},
	{ID: "cursor", DisplayName: "Cursor", Category: CategoryHarness},
	{ID: "codex", DisplayName: "Codex", Category: CategoryHarness},
	{ID: "gemini-cli", DisplayName: "Gemini CLI", Category: CategoryHarness},
	{ID: "windsurf", DisplayName: "Windsurf", Category: CategoryHarness},
	{ID: "aider", DisplayName: "Aider", Category: CategoryHarness},
	{ID: "opencode", DisplayName: "opencode", Category: CategoryHarness},
	{ID: "openclaw", DisplayName: "OpenClaw", Category: CategoryHarness},
	{ID: "ollama", DisplayName: "Ollama", Category: CategoryLocalModel},
	{ID: "lmstudio", DisplayName: "LM Studio", Category: CategoryLocalModel},
}

var targetsByID = sync.OnceValue(func() map[string]Target {
	byID := make(map[string]Target, len(targets))
	for _, target := range targets {
		byID[target.ID] = target
	}
	return byID
})

// Targets returns the catalog target list as a copy.
func Targets() []Target {
	out := make([]Target, len(targets))
	copy(out, targets)
	return out
}

// ByID resolves a catalog target by id. Ids the catalog does not know — for
// example from an agent binary compiled with a newer target list — resolve
// to ok=false; callers fall back to the raw reported values.
func ByID(id string) (Target, bool) {
	target, ok := targetsByID()[id]
	return target, ok
}
