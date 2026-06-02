// Package naming holds the marketplace + observability-plugin name formulas
// shared between the plugin publish path (server/internal/plugins) and the
// device-agent endpoint (server/internal/agent + mv).
//
// These names are a cross-surface CONTRACT, not an implementation detail.
// Claude Code (and Cursor/Codex) identify a marketplace by the "name" field in
// its published marketplace.json, and reference plugins as "<plugin>@<name>".
// So the agent endpoint MUST emit the exact same marketplace name the publish
// path wrote — otherwise the agent's enabledPlugins entries reference a
// marketplace Claude Code has never heard of and silently fail to enable.
// Keeping both sides on these functions makes that contract un-driftable.
package naming

import "github.com/speakeasy-api/gram/server/internal/conv"

// MarketplaceName is the marketplace.json "name" for an org's published
// marketplace.
func MarketplaceName(orgName string) string {
	return conv.ToSlug(orgName) + "-gram"
}

// ObservabilitySlug is the slug of the always-required observability plugin
// synthesized into every published marketplace (the Claude Code variant; the
// Cursor/Codex variants append their own suffix to this).
func ObservabilitySlug(orgName string) string {
	return conv.ToSlug(orgName) + "-observability"
}
