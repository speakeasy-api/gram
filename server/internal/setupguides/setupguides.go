// Package setupguides answers which public setup guides ship in the pinned
// mcp-setup-docs export embedded in the server binary.
//
// Answers reflect the pinned module version only. A guide merged in the docs
// repository after that version is invisible here until the server bumps the
// dependency, so callers use these lookups for advice, never as proof that a
// guide is absent.
package setupguides

import (
	guides "github.com/speakeasy-api/mcp-setup-docs/go"
)

const (
	// ModulePath and PinnedVersion identify the embedded export. The version is
	// declared here rather than read from the build's module graph because a
	// test binary carries no module graph, so a version nothing can assert
	// silently rots. A test pins this constant to go.mod, so a dependency bump
	// that forgets it fails CI.
	ModulePath    = "github.com/speakeasy-api/mcp-setup-docs/go"
	PinnedVersion = "v0.3.9"
)

// Guide identifies one published setup guide.
type Guide struct {
	// Slug is the guide's directory name and stable public key.
	Slug string `json:"slug"`

	// Title is the guide's display name.
	Title string `json:"title"`
}

// List returns every guide in the pinned export, in the export's order.
func List() []Guide {
	all := guides.Guides()
	result := make([]Guide, 0, len(all))
	for _, guide := range all {
		result = append(result, Guide{Slug: string(guide.Slug), Title: guide.Title})
	}
	return result
}

// Lookup returns the guide that uses slug exactly. Aliases do not match.
func Lookup(slug string) (Guide, bool) {
	guide, ok := guides.Lookup(guides.GuideSlug(slug))
	if !ok {
		return Guide{Slug: "", Title: ""}, false
	}
	return Guide{Slug: string(guide.Slug), Title: guide.Title}, true
}

// ForEndpoint returns the guide that documents endpoint. It reports false
// when no guide or more than one guide lists the endpoint, so an ambiguous
// URL never picks a guide for the caller.
func ForEndpoint(endpoint string) (Guide, bool) {
	slug, ok := uniqueGuide(guides.ByURL(endpoint))
	if !ok {
		return Guide{Slug: "", Title: ""}, false
	}
	return Lookup(string(slug))
}

// uniqueGuide reports the single guide named by matches. Several remotes of
// one guide count as one guide.
func uniqueGuide(matches []guides.Match) (guides.GuideSlug, bool) {
	var slug guides.GuideSlug
	for _, match := range matches {
		if slug != "" && match.Ref.Guide != slug {
			return "", false
		}
		slug = match.Ref.Guide
	}
	return slug, slug != ""
}
