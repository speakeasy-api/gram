package externalmcp

import (
	guides "github.com/speakeasy-api/mcp-setup-docs/go"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// setupGuideMatchKind maps the guides SDK's match classification onto the wire
// enum, exposing only the two kinds the endpoint's inputs name: an endpoint URL
// for server_url, a registry alias for registry_specifier. The SDK indexes
// guides three further ways, and each would answer under a key that does not
// mean it:
//
//   - Provenance is keyed by the section titles of the upstream docs a guide was
//     derived from ("API", "OAuth", "Speakeasy setup canonical section"), so it
//     can only fire by accident: it answered a lookup for one vendor's server
//     with another vendor's guide, and a single title pulls in as many as 15
//     guides at once.
//   - A guide slug and the SDK's canonical "slug/remote-id" ref identify a guide
//     in the docs catalog, not a server in a registry, which is what
//     registry_specifier means everywhere else in this service.
func setupGuideMatchKind(kind guides.MatchKind) (wire string, ok bool) {
	switch kind {
	case guides.MatchEndpoint:
		return "endpoint", true
	case guides.MatchAlias:
		return "alias", true
	default:
		return "", false
	}
}

// resolveSetupGuides locates published setup guides for an upstream MCP server
// by registry identifier and/or endpoint URL. Either input may be empty.
//
// Both inputs can match the same guide, so results are deduplicated by slug: the
// most specific match wins its match_kind, and the endpoint it selected lands in
// matched_remote_id.
func resolveSetupGuides(registrySpecifier, serverURL, callbackURL string) []*types.MCPSetupGuide {
	// ByURL is the only source of endpoint matches and Resolve the only source of
	// alias matches, so this order is descending specificity and the first match
	// for a guide is its most specific one.
	var matches []guides.Match
	if serverURL != "" {
		matches = append(matches, guides.ByURL(serverURL)...)
	}
	if registrySpecifier != "" {
		matches = append(matches, guides.Resolve(registrySpecifier)...)
	}

	views := make([]*types.MCPSetupGuide, 0, len(matches))
	bySlug := make(map[guides.GuideSlug]*types.MCPSetupGuide, len(matches))

	for _, match := range matches {
		wire, ok := setupGuideMatchKind(match.Kind)
		if !ok {
			continue
		}

		view, seen := bySlug[match.Ref.Guide]
		if !seen {
			guide, found := guides.Lookup(match.Ref.Guide)
			if !found {
				continue
			}

			view = toMCPSetupGuide(guide, wire, callbackURL)
			bySlug[match.Ref.Guide] = view
			views = append(views, view)
		}

		// An alias match carries no remote, so this stays nil for it.
		if view.MatchedRemoteID == nil {
			view.MatchedRemoteID = conv.PtrEmpty(string(match.Ref.Remote))
		}
	}

	return views
}

func toMCPSetupGuide(guide guides.Guide, matchKind, callbackURL string) *types.MCPSetupGuide {
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

	vars := guides.Vars{OAuthCallbackURL: callbackURL}

	return &types.MCPSetupGuide{
		Slug:              string(guide.Slug),
		Title:             guide.Title,
		Summary:           guide.Summary,
		AddServerFlow:     conv.PtrEmpty(guide.SpeakeasyAddServer),
		Aliases:           aliases,
		Remotes:           remotes,
		MatchedRemoteID:   nil,
		MatchKind:         matchKind,
		ExternalMarkdown:  string(guide.RenderExternal(vars)),
		SpeakeasyMarkdown: string(guide.RenderSpeakeasy(vars)),
	}
}
