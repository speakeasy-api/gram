package externalmcp

import (
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/gen/types"
)

const (
	pulseCatalogHost = "api.pulsemcp.com"
	catalogListCap   = 100
)

// pinnedRegistrySpecifiers are always merged into a Pulse catalogue listing
// even when they fall outside the page-bound crawl. Newly launched official
// remotes often rank below the ~100-server cap; pinning keeps them installable
// from the dashboard without waiting on popularity.
var pinnedRegistrySpecifiers = []string{
	"com.pulsemcp.mirror/salesforce-platform",
}

func pinnedSpecifierSet() map[string]struct{} {
	set := make(map[string]struct{}, len(pinnedRegistrySpecifiers))
	for _, specifier := range pinnedRegistrySpecifiers {
		set[specifier] = struct{}{}
	}
	return set
}

func isPulseCatalogRegistry(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), pulseCatalogHost)
}

func isPinnedRegistrySpecifier(specifier string) bool {
	_, ok := pinnedSpecifierSet()[specifier]
	return ok
}

// mergePinnedServers appends any missing pinned specifiers fetched by fetch.
// A fetch error leaves that specifier out; the rest of the catalogue is
// unchanged. fetch is not called for specifiers already present.
func mergePinnedServers(existing []*types.ExternalMCPServerEntry, fetch func(specifier string) (*types.ExternalMCPServerEntry, error)) []*types.ExternalMCPServerEntry {
	present := make(map[string]struct{}, len(existing))
	for _, server := range existing {
		if server == nil {
			continue
		}
		present[server.RegistrySpecifier] = struct{}{}
	}

	out := existing
	for _, specifier := range pinnedRegistrySpecifiers {
		if _, ok := present[specifier]; ok {
			continue
		}
		entry, err := fetch(specifier)
		if err != nil || entry == nil || entry.RegistrySpecifier != specifier {
			continue
		}
		out = append(out, entry)
		present[specifier] = struct{}{}
	}
	return out
}

// capCatalogServers trims a sorted catalogue to limit while keeping every
// pinned specifier that the listing actually contains. Pinned overflow
// displaces trailing non-pinned entries so a newly launched official remote
// cannot fall off the public cap.
func capCatalogServers(servers []*types.ExternalMCPServerEntry, limit int) []*types.ExternalMCPServerEntry {
	if limit <= 0 || len(servers) <= limit {
		return servers
	}

	pinned := pinnedSpecifierSet()
	overflow := 0
	for _, server := range servers[limit:] {
		if server != nil {
			if _, ok := pinned[server.RegistrySpecifier]; ok {
				overflow++
			}
		}
	}
	if overflow == 0 {
		return servers[:limit]
	}

	window := servers[:limit]
	dropIdx := make(map[int]struct{}, overflow)
	dropped := 0
	for i := len(window) - 1; i >= 0 && dropped < overflow; i-- {
		if window[i] == nil {
			continue
		}
		if _, ok := pinned[window[i].RegistrySpecifier]; ok {
			continue
		}
		dropIdx[i] = struct{}{}
		dropped++
	}

	kept := make([]*types.ExternalMCPServerEntry, 0, limit)
	for i, server := range window {
		if _, ok := dropIdx[i]; ok {
			continue
		}
		kept = append(kept, server)
	}
	for _, server := range servers[limit:] {
		if server == nil {
			continue
		}
		if _, ok := pinned[server.RegistrySpecifier]; ok {
			kept = append(kept, server)
		}
	}
	if len(kept) > limit {
		return kept[:limit]
	}
	return kept
}
