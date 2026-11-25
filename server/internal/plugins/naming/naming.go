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
// Customer managed settings key extraKnownMarketplaces and the enabledPlugins
// "@" suffix by that name too, so a published name is FROZEN: once a project
// has published, only an explicit override (or clearing one) renames it. An
// org rename, a project slug change, or a change of the org's default project
// must never move it. Keeping every surface on ResolveMarketplaceName makes
// that contract un-driftable.
package naming

import (
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// PublishedMarketplaceNameKey is the published hooks config snapshot key that
// records the marketplace.json name a project's repo was last published under.
// It sits beside the hooks config fields but is deliberately not one of them:
// the publish path hashes only those fields to decide whether to regenerate the
// hooks subtree, so recording or correcting the published name never reads as
// a hooks change.
const PublishedMarketplaceNameKey = "published_marketplace_name"

// MarketplaceName is the computed marketplace.json "name" for a project: the
// name it gets on its first publish, before ResolveMarketplaceName freezes it.
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
// by created_at then id. Otherwise the two compute different names for a
// project's first publish and silently fail to match.
func MarketplaceName(orgName, projectSlug string, isDefaultProject bool) string {
	base := conv.ToSlug(orgName)
	slug := conv.ToSlug(projectSlug)
	if isDefaultProject || slug == "" {
		return base + "-speakeasy"
	}
	return base + "-" + slug + "-speakeasy"
}

// ResolveMarketplaceName is the marketplace name a project publishes under,
// resolved the same way on every surface (generated files, the dashboard, the
// device-agent endpoint):
//
//  1. override, the explicit per-project name an admin set;
//  2. otherwise the name the project last published under, recorded in its
//     published hooks config snapshot;
//  3. otherwise the computed MarketplaceName, for a project that has not
//     published since names were recorded.
func ResolveMarketplaceName(override string, publishedSnapshot []byte, orgName, projectSlug string, isDefaultProject bool) string {
	if override != "" {
		return override
	}
	return DefaultMarketplaceName(override, publishedSnapshot, orgName, projectSlug, isDefaultProject)
}

// DefaultMarketplaceName is the name a project publishes under without an
// override, and so the name it returns to when its override is cleared: the
// name it last published under, else the computed MarketplaceName. A recorded
// name equal to the current override came from that override rather than from
// the project's default, so it is skipped and clearing a published override
// returns to the computed name. A recorded name that differs from the override
// is still live in the repo (the override has not published yet), so clearing
// the override keeps it.
func DefaultMarketplaceName(override string, publishedSnapshot []byte, orgName, projectSlug string, isDefaultProject bool) string {
	if published := PublishedMarketplaceName(publishedSnapshot); published != "" && published != override {
		return published
	}
	return MarketplaceName(orgName, projectSlug, isDefaultProject)
}

// ObservabilitySlug is the slug of the observability plugin synthesized into
// a published marketplace when the project has not disabled it (the Claude
// Code variant; the Cursor/Codex variants append their own suffix to this).
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

// PublishedMarketplaceName extracts the marketplace.json name a project's repo
// was last published under from the connection's stored hooks config snapshot.
// Empty when the snapshot is missing, unreadable, or was last written before
// the name was recorded; ResolveMarketplaceName then falls back to the computed
// name, which is the name those projects publish under today.
func PublishedMarketplaceName(snapshot []byte) string {
	var recorded struct {
		Name string `json:"published_marketplace_name"`
	}
	if err := json.Unmarshal(snapshot, &recorded); err != nil {
		return ""
	}
	return recorded.Name
}

// WithPublishedMarketplaceName returns snapshot with name recorded as the
// published marketplace name. Every other key is kept verbatim, so a carried
// hooks config snapshot still describes the hooks subtree it was generated
// with. A missing snapshot (a project that does not ship the observability
// plugin) becomes an object holding only the name.
func WithPublishedMarketplaceName(snapshot []byte, name string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if len(snapshot) > 0 {
		if err := json.Unmarshal(snapshot, &fields); err != nil {
			return nil, fmt.Errorf("decode published hooks config: %w", err)
		}
	}
	// A JSON null snapshot decodes to a nil map, the same as no snapshot.
	if fields == nil {
		fields = make(map[string]json.RawMessage, 1)
	}
	encodedName, err := json.Marshal(name)
	if err != nil {
		return nil, fmt.Errorf("encode published marketplace name: %w", err)
	}
	fields[PublishedMarketplaceNameKey] = encodedName
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("encode published hooks config: %w", err)
	}
	return out, nil
}
