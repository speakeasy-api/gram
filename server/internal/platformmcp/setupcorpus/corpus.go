// Package setupcorpus builds the reviewed Platform MCP setup resource corpus
// from the pinned mcp-setup-docs export.
//
// The corpus is a compile-time artifact. mcp-setup-docs ships as a Go module
// whose guides are embedded in the binary, so "pin a reviewed export" and "pin
// a module version" are the same act, and the runtime has no code path that
// could read GitHub, a provider's pages, or an unreviewed search result. A
// corpus change is a version bump, reviewed and deployed like any other
// dependency change.
package setupcorpus

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	guides "github.com/speakeasy-api/mcp-setup-docs/go"
	"gopkg.in/yaml.v3"

	"github.com/speakeasy-api/gram/server/internal/platformmcp"
)

const (
	// ModulePath and PinnedVersion identify the reviewed export every citation
	// names. The version is declared here rather than read from the build's
	// module graph because a test binary carries no module graph — a version
	// nothing can assert is a version that silently rots. A test pins this
	// constant to go.mod, so a dependency bump that forgets the citation fails
	// CI instead of shipping a guide that cites the wrong export.
	ModulePath    = "github.com/speakeasy-api/mcp-setup-docs/go"
	PinnedVersion = "v0.3.0"
	// sourceName is the human half of a citation: "mcp-setup-docs/go@v0.3.0".
	sourceName = "mcp-setup-docs/go"

	// Owner is who is accountable for reviewing this content. mcp-setup-docs
	// has no per-guide reviewer field yet, so ownership is stated at the corpus
	// level rather than invented per guide.
	Owner = "Speakeasy mcp-setup-docs maintainers"

	// ProviderSetupIntent covers what the reader does at the provider —
	// registering an OAuth app, finding a tenant URL, granting access.
	// ControlPlaneSetupIntent covers what they do in the AI Control Plane.
	// They are separate intents because they are separate jobs, usually done by
	// different people, and a reader asking about one should not have to read
	// past the other.
	ProviderSetupIntent     = "provider_setup"
	ControlPlaneSetupIntent = "control_plane_setup"

	// revalidationWindow is how long an observation of a provider's
	// documentation is treated as current. mcp-setup-docs records when it
	// observed a source but not when a human last re-read the guide, so the
	// revalidation date is derived from the observation. That makes it a
	// freshness bound, not a review record: promoting an explicit reviewed_at
	// upstream is what would let this stop being derived.
	revalidationWindow = 90 * 24 * time.Hour

	// docsBaseURL is where the published guides live on the documentation
	// site. Every guide is served at its slug, so the page URL is derivable
	// rather than a second thing to keep in sync; a slug with no page would
	// show up as a broken link in CI's link check.
	docsBaseURL = "https://www.speakeasy.com/docs/ai-control-plane/guides/"

	// maxLinks bounds the canonical links carried per resource. Provenance runs
	// to fifteen entries for some providers, which is a citation list nobody
	// reads and 2 KiB of every excerpt spent on URLs.
	maxLinks = 6
)

// Options are the deployment-specific values the corpus is rendered with.
type Options struct {
	// OAuthCallbackURL is this deployment's callback URL, substituted into the
	// guides' template key. An empty value leaves the key visible rather than
	// blanking it, which is a legible defect instead of a silent one.
	OAuthCallbackURL string
}

// guideMeta is the subset of a guide's meta.yaml this package reads. It is
// deliberately partial: fields the corpus does not cite are not decoded, so an
// upstream schema addition cannot break the build.
type guideMeta struct {
	Provenance []struct {
		Source         string `yaml:"source"`
		Locator        string `yaml:"locator"`
		Name           string `yaml:"name"`
		Classification string `yaml:"classification"`
		ObservedAt     string `yaml:"observed_at"`
	} `yaml:"provenance"`
}

// Build renders every published guide into reviewed setup resources.
//
// It fails rather than skipping: a guide that cannot be turned into a valid
// resource means the pinned export and this code disagree, and serving the
// remainder would hide that behind a corpus that silently lost a provider.
func Build(opts Options) ([]platformmcp.SetupResource, error) {
	vars := guides.Vars{OAuthCallbackURL: opts.OAuthCallbackURL}

	resources := make([]platformmcp.SetupResource, 0, len(guides.Slugs())*2)
	for _, guide := range guides.Guides() {
		observedAt, links, err := provenance(guide)
		if err != nil {
			return nil, fmt.Errorf("read provenance for setup guide %q: %w", guide.Slug, err)
		}

		for _, section := range []struct {
			intent  string
			label   string
			summary string
			body    []byte
		}{
			{
				intent:  ProviderSetupIntent,
				label:   "provider setup",
				summary: "what to do at the provider",
				body:    guide.RenderExternal(vars),
			},
			{
				intent:  ControlPlaneSetupIntent,
				label:   "AI Control Plane setup",
				summary: "what to do in the Speakeasy AI Control Plane",
				body:    guide.RenderSpeakeasy(vars),
			},
		} {
			// Stripped before the emptiness check: a section carrying only front
			// matter has no guide content, and serving it would publish a
			// resource that is nothing but its own citation header.
			body := stripFrontMatter(string(section.body))
			if strings.TrimSpace(body) == "" {
				continue
			}
			resource := platformmcp.SetupResource{
				URI:          platformmcp.SetupResourceURI(string(guide.Slug), section.intent),
				Name:         fmt.Sprintf("setup-%s-%s", guide.Slug, strings.ReplaceAll(section.intent, "_", "-")),
				Title:        fmt.Sprintf("%s — %s", guide.Title, section.label),
				Description:  fmt.Sprintf("Reviewed %s guide for %s: %s.", section.label, guide.Title, section.summary),
				Text:         "",
				Body:         body,
				Provider:     string(guide.Slug),
				Intent:       section.intent,
				Owner:        Owner,
				Source:       sourceName + "@" + PinnedVersion,
				ObservedAt:   observedAt,
				RevalidateBy: observedAt.Add(revalidationWindow),
				Aliases:      guide.Aliases,
				Links:        links,
				DocsURL:      docsBaseURL + string(guide.Slug),
			}
			resource.Text = header(resource, guide) + body

			if err := platformmcp.ValidateSetupResource(resource); err != nil {
				return nil, fmt.Errorf("build setup resource %s: %w", resource.URI, err)
			}
			resources = append(resources, resource)
		}
	}
	return resources, nil
}

// header prefixes each guide with its own citation, so a reader who was handed
// the content without the surrounding tool result can still see who owns it,
// which pinned export it came from, and when it was last observed.
func header(resource platformmcp.SetupResource, guide guides.Guide) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", resource.Title)
	fmt.Fprintf(&b, "- Owner: %s\n", resource.Owner)
	fmt.Fprintf(&b, "- Source: %s (pinned reviewed export; never fetched at request time)\n", resource.Source)
	fmt.Fprintf(&b, "- Observed: %s\n", resource.ObservedAt.Format(time.DateOnly))
	fmt.Fprintf(&b, "- Revalidate by: %s\n", resource.RevalidateBy.Format(time.DateOnly))
	fmt.Fprintf(&b, "- Speakeasy docs: %s\n", resource.DocsURL)
	if len(guide.Aliases) > 0 {
		fmt.Fprintf(&b, "- Also known as: %s\n", strings.Join(guide.Aliases, ", "))
	}
	if len(resource.Links) > 0 {
		b.WriteString("- Canonical sources:\n")
		for _, link := range resource.Links {
			fmt.Fprintf(&b, "  - %s\n", link)
		}
	}
	b.WriteString("\n---\n\n")
	return b.String()
}

// stripFrontMatter removes a guide's authoring front matter. It carries a
// content schema version for the generator, which means nothing to a reader and
// reads as a stray rule right under the citation block.
func stripFrontMatter(body string) string {
	trimmed := strings.TrimLeft(body, "\n")
	if !strings.HasPrefix(trimmed, "---\n") {
		return body
	}
	_, rest, found := strings.Cut(trimmed[len("---\n"):], "\n---\n")
	if !found {
		return body
	}
	return strings.TrimLeft(rest, "\n")
}

// followableHTTPS reports whether a provenance locator is somewhere a reader
// can actually be sent. A prefix test alone would pass "https://" with no host,
// or a string that only looks like a URL, and publish it as a citation.
func followableHTTPS(locator string) bool {
	parsed, err := url.Parse(locator)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

// provenance returns the guide's oldest observation and its canonical links.
//
// The oldest observation is the honest one: a guide is only as current as the
// least recently checked source behind it, and taking the newest would let one
// re-checked link keep a stale guide alive.
func provenance(guide guides.Guide) (time.Time, []string, error) {
	var meta guideMeta
	if err := yaml.Unmarshal(guide.Meta, &meta); err != nil {
		return time.Time{}, nil, fmt.Errorf("parse meta.yaml: %w", err)
	}
	if len(meta.Provenance) == 0 {
		return time.Time{}, nil, fmt.Errorf("guide declares no provenance")
	}

	var oldest time.Time
	links := make([]string, 0, maxLinks)
	seen := make(map[string]bool, len(meta.Provenance))
	for _, entry := range meta.Provenance {
		observedAt, err := time.Parse(time.RFC3339, entry.ObservedAt)
		if err != nil {
			return time.Time{}, nil, fmt.Errorf("parse observed_at %q: %w", entry.ObservedAt, err)
		}
		if oldest.IsZero() || observedAt.Before(oldest) {
			oldest = observedAt
		}

		// Only external, official sources are offered as links a reader may
		// follow. Repository-internal doctrine locators are provenance for the
		// authors, not somewhere to send a user.
		if len(links) == maxLinks || entry.Classification != "official" || seen[entry.Locator] || !followableHTTPS(entry.Locator) {
			continue
		}
		seen[entry.Locator] = true
		links = append(links, entry.Locator)
	}
	return oldest, links, nil
}
