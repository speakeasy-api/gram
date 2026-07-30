package externalmcp

import (
	"slices"

	guides "github.com/speakeasy-api/mcp-setup-docs/go"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// setupGuideMatchKinds maps the guides SDK's match classification onto the wire
// enum. An unrecognised kind is reported as "slug", the least specific
// guide-level match, so a classification added by a future SDK release degrades
// instead of leaking an empty string into a response the SDK enum rejects.
var setupGuideMatchKinds = map[guides.MatchKind]string{
	guides.MatchServerRef:  "server_ref",
	guides.MatchSlug:       "slug",
	guides.MatchAlias:      "alias",
	guides.MatchProvenance: "provenance",
	guides.MatchEndpoint:   "endpoint",
}

// resolveSetupGuides locates published setup guides for an upstream MCP server
// by registry identifier and/or endpoint URL. Either input may be empty.
//
// Both inputs can match the same guide, and one input can match several guides
// (a provenance name is shared by every server documented under it), so results
// are deduplicated by slug. The first match for a guide wins its match_kind,
// which makes it the most specific one: the SDK resolves in descending
// specificity, and the caller's identifier is checked ahead of the URL.
func resolveSetupGuides(registrySpecifier, serverURL string) []*types.MCPSetupGuide {
	var matches []guides.Match
	if registrySpecifier != "" {
		matches = append(matches, guides.Resolve(registrySpecifier)...)
	}
	if serverURL != "" {
		matches = append(matches, guides.ByURL(serverURL)...)
	}

	views := make([]*types.MCPSetupGuide, 0, len(matches))
	bySlug := make(map[guides.GuideSlug]*types.MCPSetupGuide, len(matches))

	for _, match := range matches {
		view, seen := bySlug[match.Ref.Guide]
		if !seen {
			guide, ok := guides.Lookup(match.Ref.Guide)
			if !ok {
				continue
			}

			view = toMCPSetupGuide(guide, match.Kind)
			bySlug[match.Ref.Guide] = view
			views = append(views, view)
		}

		// Guide-level matches (slug, alias) select no specific endpoint.
		remoteID := string(match.Ref.Remote)
		if remoteID != "" && !slices.Contains(view.MatchedRemoteIds, remoteID) {
			view.MatchedRemoteIds = append(view.MatchedRemoteIds, remoteID)
		}
	}

	return views
}

func toMCPSetupGuide(guide guides.Guide, kind guides.MatchKind) *types.MCPSetupGuide {
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
		MatchKind:         conv.Default(setupGuideMatchKinds[kind], "slug"),
		ExternalMarkdown:  string(guide.External),
		SpeakeasyMarkdown: string(guide.Speakeasy),
	}
}
