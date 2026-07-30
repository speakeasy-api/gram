package externalmcp

import (
	"slices"

	guides "github.com/speakeasy-api/mcp-setup-docs/go"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// setupGuideMatchKind maps the guides SDK's match classification onto the wire
// enum, paired with a specificity rank (lower is more specific).
//
// Two SDK kinds are deliberately not exposed. guides.MatchProvenance is keyed by
// the section titles of the upstream docs a guide was derived from ("API",
// "OAuth", "Speakeasy setup canonical section"), not by anything a caller holds
// as a server identifier, so it can only ever fire by accident: it would answer
// a lookup for one vendor's server with another vendor's guide, and a single
// title can pull in most of the catalog at once. A kind added by a future SDK
// release reports ok=false for the same reason: an unrecognised match is dropped
// rather than reported under a kind it isn't.
func setupGuideMatchKind(kind guides.MatchKind) (wire string, rank int, ok bool) {
	switch kind {
	case guides.MatchServerRef:
		return "server_ref", 0, true
	case guides.MatchEndpoint:
		return "endpoint", 1, true
	case guides.MatchSlug:
		return "slug", 2, true
	case guides.MatchAlias:
		return "alias", 3, true
	default:
		return "", 0, false
	}
}

// resolveSetupGuides locates published setup guides for an upstream MCP server
// by registry identifier and/or endpoint URL. Either input may be empty.
//
// Both inputs can match the same guide, so results are deduplicated by slug: the
// most specific match for a guide wins its match_kind, and every endpoint the
// lookup selected for it lands in matched_remote_ids. Guides are returned in
// descending specificity, which has to be sorted for rather than inherited from
// the SDK's check order: that order is per-lookup-key, so without a sort a loose
// match on the identifier would outrank an exact endpoint match on the URL.
func resolveSetupGuides(registrySpecifier, serverURL string) []*types.MCPSetupGuide {
	var matches []guides.Match
	if registrySpecifier != "" {
		matches = append(matches, guides.Resolve(registrySpecifier)...)
	}
	if serverURL != "" {
		matches = append(matches, guides.ByURL(serverURL)...)
	}

	type rankedGuide struct {
		view *types.MCPSetupGuide
		rank int
	}

	ranked := make([]rankedGuide, 0, len(matches))
	bySlug := make(map[guides.GuideSlug]int, len(matches))

	for _, match := range matches {
		wire, rank, ok := setupGuideMatchKind(match.Kind)
		if !ok {
			continue
		}

		i, seen := bySlug[match.Ref.Guide]
		if !seen {
			guide, found := guides.Lookup(match.Ref.Guide)
			if !found {
				continue
			}

			i = len(ranked)
			bySlug[match.Ref.Guide] = i
			ranked = append(ranked, rankedGuide{view: toMCPSetupGuide(guide, wire), rank: rank})
		} else if rank < ranked[i].rank {
			ranked[i].rank = rank
			ranked[i].view.MatchKind = wire
		}

		// Guide-level matches (slug, alias) select no specific endpoint.
		remoteID := string(match.Ref.Remote)
		if remoteID != "" && !slices.Contains(ranked[i].view.MatchedRemoteIds, remoteID) {
			ranked[i].view.MatchedRemoteIds = append(ranked[i].view.MatchedRemoteIds, remoteID)
		}
	}

	slices.SortStableFunc(ranked, func(a, b rankedGuide) int { return a.rank - b.rank })

	views := make([]*types.MCPSetupGuide, 0, len(ranked))
	for _, r := range ranked {
		views = append(views, r.view)
	}

	return views
}

func toMCPSetupGuide(guide guides.Guide, matchKind string) *types.MCPSetupGuide {
	remotes := make([]*types.MCPSetupGuideRemote, 0, len(guide.Remotes))
	for _, remote := range guide.Remotes {
		remotes = append(remotes, &types.MCPSetupGuideRemote{
			ID:            string(remote.ID),
			URL:           remote.URL,
			TransportType: remote.Transport,
			Tenanted:      remote.Tenanted,
		})
	}

	// Required array attributes are emitted as [] rather than null so clients
	// don't have to special-case an absent list.
	aliases := make([]string, 0, len(guide.Aliases))
	aliases = append(aliases, guide.Aliases...)

	return &types.MCPSetupGuide{
		Slug:              string(guide.Slug),
		Title:             guide.Title,
		Summary:           guide.Summary,
		AddServerFlow:     conv.PtrEmpty(guide.SpeakeasyAddServer),
		Aliases:           aliases,
		Remotes:           remotes,
		MatchedRemoteIds:  []string{},
		MatchKind:         matchKind,
		ExternalMarkdown:  string(guide.External),
		SpeakeasyMarkdown: string(guide.Speakeasy),
	}
}
