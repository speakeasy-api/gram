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

import (
	"encoding/json"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// MarketplaceName is the marketplace.json "name" for a project's published
// marketplace.
//
// An org can publish multiple projects, each its own marketplace, and a
// marketplace.json name is a single identifier that must be unique on the
// device. So names are project-scoped: `<org>-<project>-speakeasy`. The one
// exception is the org's default project (and the no-project fallback), which
// keeps the bare `<org>-speakeasy` name it has always had — so existing installs
// for single-project orgs don't churn when this scoping lands; only an org's
// non-default projects get a new, distinct name.
//
// isDefaultProject must be resolved the same way on both surfaces (the publish
// path and the device-agent endpoint) — the org's oldest non-deleted project,
// by id ASC — or the two will compute different names and silently fail to match.
func MarketplaceName(orgName, projectSlug string, isDefaultProject bool) string {
	base := conv.ToSlug(orgName)
	slug := conv.ToSlug(projectSlug)
	if isDefaultProject || slug == "" {
		return base + "-speakeasy"
	}
	return base + "-" + slug + "-speakeasy"
}

// ObservabilitySlug is the slug of the always-required observability plugin
// synthesized into every published marketplace (the Claude Code variant; the
// Cursor/Codex variants append their own suffix to this).
func ObservabilitySlug(orgName string) string {
	return conv.ToSlug(orgName) + "-observability"
}

// PublishedHooksOrgName extracts the org name a published hooks subtree was
// generated under from the connection's stored hooks config snapshot (written
// by the publish path as plugins.HooksConfig — the org_name tag must stay in
// sync with that struct). The hooks rollout gate can pin a published subtree
// under a pre-rename org name, so every surface that names the published
// observability plugin must feed this into the slug formulas above rather
// than the current org name. Empty when the snapshot is missing, unreadable,
// or predates the field; callers fall back to the current org name.
func PublishedHooksOrgName(snapshot []byte) string {
	var hc struct {
		OrgName string `json:"org_name"`
	}
	if err := json.Unmarshal(snapshot, &hc); err != nil {
		return ""
	}
	return hc.OrgName
}
