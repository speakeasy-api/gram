// Package servernames folds the server names agent hooks report back onto the
// MCP servers configured in a project.
//
// A hook records the name the calling agent used for a server, not the
// configured server. Claude Code derives "plugin_<plugin>_<Display_Name>" for
// a server shipped in a plugin, a hook that resolved the server against its
// inventory records the display name, other clients report a bare slug, and
// some report the configured id. The resolver knows every name a configured
// server can be reported under and maps a reported name back to exactly one
// server, refusing a name that could belong to more than one.
package servernames

import (
	"sort"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/toolref"
)

// PluginMembership is one plugin a configured server is distributed through.
type PluginMembership struct {
	// PluginSlug is the plugin's manifest name, which Claude Code embeds in
	// the tool prefix it derives for the plugin's servers.
	PluginSlug string

	// DisplayName is the key the server is written under in the plugin's
	// mcp.json, and the name a hook that resolved the server reports.
	DisplayName string
}

// ConfiguredServer is one MCP server as configured in a project.
type ConfiguredServer struct {
	// ID is the configured server's id.
	ID string

	// Name is the server's human-readable name, or empty.
	Name string

	// Slug is the server's URL slug, or empty.
	Slug string

	// ToolsetSlug is the hosted toolset the server fronts, or empty for a
	// remote, tunneled, or unproxied server.
	ToolsetSlug string

	// Plugins lists the plugin memberships the server is distributed through.
	Plugins []PluginMembership
}

// Resolver maps reported server names to configured servers. The zero value
// and a nil pointer resolve nothing.
type Resolver struct {
	// byKey maps a normalized reported name to the id of the one server it
	// identifies. An empty id marks a name two servers share, which resolves
	// to neither.
	byKey map[string]string

	// names maps a server id to the name results report it under.
	names map[string]string

	// reported maps a server id to every lower-cased spelling that uniquely
	// identifies it, for matching raw telemetry.
	reported map[string][]string
}

// NewResolver indexes every name each configured server can be reported
// under: its id, slug, toolset slug, and name, plus each plugin membership's
// display name and the tool prefix Claude Code derives from it.
func NewResolver(servers []ConfiguredServer) *Resolver {
	r := &Resolver{
		byKey:    map[string]string{},
		names:    map[string]string{},
		reported: map[string][]string{},
	}
	candidates := map[string][]string{}
	order := make([]string, 0, len(servers))
	for _, server := range servers {
		if server.ID == "" {
			continue
		}
		if _, seen := r.names[server.ID]; !seen {
			order = append(order, server.ID)
		}
		r.names[server.ID] = displayName(server)
		raw := []string{server.ID, server.Slug, server.ToolsetSlug, server.Name}
		for _, membership := range server.Plugins {
			raw = append(raw, membership.DisplayName)
			if membership.PluginSlug != "" && membership.DisplayName != "" {
				raw = append(raw, toolref.ClaudeMCPServerPrefix(toolref.ClaudeMCPSourcePlugin, membership.PluginSlug, membership.DisplayName))
			}
		}
		for _, name := range raw {
			key := normalize(name)
			if key == "" {
				continue
			}
			candidates[server.ID] = append(candidates[server.ID], name)
			if owner, seen := r.byKey[key]; seen && owner != server.ID {
				r.byKey[key] = ""
				continue
			}
			r.byKey[key] = server.ID
		}
	}
	for _, id := range order {
		seen := map[string]bool{}
		for _, name := range candidates[id] {
			key := normalize(name)
			if r.byKey[key] != id {
				continue
			}
			for _, spelling := range []string{strings.ToLower(strings.TrimSpace(name)), key} {
				if spelling == "" || seen[spelling] {
					continue
				}
				seen[spelling] = true
				r.reported[id] = append(r.reported[id], spelling)
			}
		}
		sort.Strings(r.reported[id])
	}
	return r
}

// Resolve returns the configured server a reported name identifies. It
// reports false for a name no configured server is known by and for a name
// more than one server shares, so a caller never attributes traffic by guess.
func (r *Resolver) Resolve(reported string) (string, bool) {
	if r == nil {
		return "", false
	}
	id, ok := r.byKey[normalize(reported)]
	return id, ok && id != ""
}

// Name returns how results name a configured server: its configured name,
// else its slug, else its id. An unknown id returns itself.
func (r *Resolver) Name(id string) string {
	if r == nil {
		return id
	}
	if name, ok := r.names[id]; ok {
		return name
	}
	return id
}

// ReportedNames returns every spelling, lower-cased, that uniquely identifies
// the server, for matching raw telemetry case-insensitively. A spelling another
// server shares is omitted so one call is never attributed to both.
func (r *Resolver) ReportedNames(id string) []string {
	if r == nil {
		return nil
	}
	names := r.reported[id]
	if len(names) == 0 {
		return nil
	}
	out := make([]string, len(names))
	copy(out, names)
	return out
}

func displayName(server ConfiguredServer) string {
	switch {
	case server.Name != "":
		return server.Name
	case server.Slug != "":
		return server.Slug
	default:
		return server.ID
	}
}

// normalize is the comparison form of a reported name: trimmed, rewritten the
// way Claude Code rewrites a server name for a tool prefix, and lower-cased,
// so "External Acme Chat", "External_Acme_Chat", and "external_acme_chat"
// compare equal.
func normalize(name string) string {
	return strings.ToLower(toolref.SanitizeClaudeMCPName(strings.TrimSpace(name)))
}
