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
	"slices"
	"sort"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/conv"
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

	// reported maps a server id to every spelling an agent may report it under
	// that uniquely identifies it, for matching raw telemetry by exact value.
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
	for _, server := range servers {
		if server.ID == "" {
			continue
		}
		r.names[server.ID] = conv.Default(server.Name, conv.Default(server.Slug, server.ID))
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
	for id, names := range candidates {
		// Each unique name is kept as configured, as Claude Code rewrites it for
		// a tool prefix, and lower-cased in both forms. The telemetry read
		// compares the indexed column by exact value, so every spelling Resolve
		// would accept has to be listed for a hook that reports it to match.
		spellings := make([]string, 0, 4*len(names))
		for _, name := range names {
			if r.byKey[normalize(name)] != id {
				continue
			}
			trimmed := strings.TrimSpace(name)
			sanitized := toolref.SanitizeClaudeMCPName(trimmed)
			spellings = append(spellings, trimmed, sanitized, strings.ToLower(trimmed), strings.ToLower(sanitized))
		}
		reported := conv.DedupeNonEmpty(spellings)
		sort.Strings(reported)
		r.reported[id] = reported
	}
	return r
}

// Resolve returns the configured server a reported name identifies. It
// reports false for a name no configured server is known by and for a name
// more than one server shares, so a caller never attributes traffic by guess.
func (r *Resolver) Resolve(reported string) (string, bool) {
	id, ok := r.byKey[normalize(reported)]
	return id, ok && id != ""
}

// Name returns how results name a configured server: its configured name,
// else its slug, else its id. An unknown id returns itself.
func (r *Resolver) Name(id string) string {
	if name, ok := r.names[id]; ok {
		return name
	}
	return id
}

// ReportedNames returns every spelling an agent may report the server under
// that uniquely identifies it: as configured, as Claude Code rewrites it for a
// tool prefix, and lower-cased. The telemetry read matches raw rows by exact
// value, so the index on the reported name applies and recall comes from
// listing every variant. A spelling another server shares is omitted so one
// call is never attributed to both.
func (r *Resolver) ReportedNames(id string) []string {
	names := r.reported[id]
	if len(names) == 0 {
		return nil
	}
	return slices.Clone(names)
}

// normalize is the comparison form of a reported name: trimmed, rewritten the
// way Claude Code rewrites a server name for a tool prefix, and lower-cased,
// so "External Acme Chat", "External_Acme_Chat", and "external_acme_chat"
// compare equal.
func normalize(name string) string {
	return strings.ToLower(toolref.SanitizeClaudeMCPName(strings.TrimSpace(name)))
}
