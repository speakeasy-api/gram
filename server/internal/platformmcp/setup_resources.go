package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrSetupGuideUnavailable is returned instead of setup content that is too far
// past its revalidation date to stand behind. Callers surface it as the
// guide_unavailable code with the guide's canonical links, so the model hands
// the reader a trusted source rather than inventing steps.
var ErrSetupGuideUnavailable = errors.New("platform mcp setup guide unavailable")

// setupGuideUnavailableCode is the wire code for withheld or missing content.
// It matches the readiness state of the same name: both mean "no reviewed guide
// stands behind this right now".
const setupGuideUnavailableCode = string(ReadinessGuideUnavailable)

const (
	maxSetupResourceBytes = 32 * 1024
	// setupResourceGraceDays is how long a guide keeps being served after its
	// revalidation date. Within the grace window a reader still gets the
	// reviewed steps, prefixed with a warning; past it the content is withheld
	// entirely, because unreviewed provider setup steps are worse than none.
	setupResourceGraceDays = 30
)

// SetupResource is a reviewed, static setup guide. It is intentionally supplied
// only by explicit composition, never fetched from a provider or documentation
// service at request time.
//
// The metadata fields are not decoration: they are the citation a reader needs
// to judge the content, and the freshness signal that decides whether it is
// served at all.
type SetupResource struct {
	URI         string
	Name        string
	Title       string
	Description string
	// Text is what a reader receives: the citation header followed by the
	// guide. Body is the guide alone.
	Text string
	// Body is what search indexes. The citation header is prose about the
	// guide rather than guide content, so indexing Text would let every guide
	// match on "owner" or "source" and would return the header block as the
	// answer to a setup question.
	Body string

	// Provider and Intent are the two URI segments, kept as fields so search
	// results can cite them without re-parsing the URI.
	Provider string
	Intent   string
	// Owner is who is accountable for reviewing this content.
	Owner string
	// Source identifies the pinned export the content came from, including its
	// version — "mcp-setup-docs/go@v0.3.0", not "mcp-setup-docs".
	Source string
	// ObservedAt is when the upstream provider documentation behind this guide
	// was last observed. RevalidateBy is when that observation expires.
	ObservedAt   time.Time
	RevalidateBy time.Time
	// Aliases are other names the provider is known by — registry identifiers,
	// vendor spellings — so search can match what a caller actually typed.
	Aliases []string
	// Links are the canonical upstream sources. They are the fallback a reader
	// is handed when content is missing or withheld.
	Links []string
	// DocsURL is this guide's published page on the Speakeasy documentation
	// site — the same content, somewhere a person can open, link, and share.
	// The gram:// URI addresses the resource; this addresses the page.
	DocsURL string
}

// indexText is the content search reads: the guide itself, falling back to the
// whole resource for a composition that supplies no separate body.
func (r SetupResource) indexText() string {
	if r.Body != "" {
		return r.Body
	}
	return r.Text
}

// staleness classifies a guide against the clock at read time.
type staleness int

const (
	// setupFresh: within its revalidation date.
	setupFresh staleness = iota
	// setupStale: past revalidation but inside the grace window — served with
	// a warning.
	setupStale
	// setupWithheld: past the grace window — not served.
	setupWithheld
)

func (r SetupResource) staleness(now time.Time) staleness {
	if r.RevalidateBy.IsZero() || !now.After(r.RevalidateBy) {
		return setupFresh
	}
	if now.After(r.RevalidateBy.AddDate(0, 0, setupResourceGraceDays)) {
		return setupWithheld
	}
	return setupStale
}

// staleWarning is prefixed to a guide that is past its revalidation date. It
// names the dates and the canonical links so the reader can check the provider
// themselves rather than trusting steps nobody has re-read.
func (r SetupResource) staleWarning() string {
	var b strings.Builder
	b.WriteString("> **Warning: this guide is past its revalidation date.**\n>\n")
	fmt.Fprintf(&b, "> Last observed %s; revalidation was due %s. Provider setup may have changed since.\n", r.ObservedAt.Format(time.DateOnly), r.RevalidateBy.Format(time.DateOnly))
	if len(r.Links) > 0 {
		b.WriteString(">\n> Verify against the canonical sources before following these steps:\n")
		for _, link := range r.Links {
			fmt.Fprintf(&b, "> - %s\n", link)
		}
	}
	b.WriteString("\n")
	return b.String()
}

// registerSetupResources registers the reviewed corpus. now is injected so a
// test can place the clock relative to a guide's revalidation date rather than
// depending on when the test happens to run.
func registerSetupResources(reg *Registrar, resources []SetupResource, now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	for _, resource := range resources {
		if !validSetupResource(resource) {
			continue
		}
		addResource(reg, &mcp.Resource{ //nolint:exhaustruct // MCP SDK metadata and annotations are intentionally omitted.
			URI:         resource.URI,
			Name:        resource.Name,
			Title:       resource.Title,
			Description: resource.Description,
			MIMEType:    "text/markdown",
			Size:        int64(len(resource.Text)),
		}, ResourceMeta{
			// Both audiences: a reviewed guide carries no connection-scoped or
			// project-scoped state, so the assistant can read exactly what an
			// external client reads. Anything less would leave the resource
			// links in a search result dangling on the assistant surface.
			Audiences: bothAudiences,
		}, func(_ context.Context) (string, error) {
			switch resource.staleness(now()) {
			case setupWithheld:
				return "", ErrSetupGuideUnavailable
			case setupStale:
				return resource.staleWarning() + resource.Text, nil
			case setupFresh:
				return resource.Text, nil
			default:
				return resource.Text, nil
			}
		})
	}
}

func validSetupResource(resource SetupResource) bool {
	return ValidateSetupResource(resource) == nil
}

// ValidateSetupResource reports why a resource is not servable. A corpus
// builder gets the reason so a malformed guide fails the build it came from,
// rather than disappearing from the corpus at registration time.
func ValidateSetupResource(resource SetupResource) error {
	parsed, err := url.Parse(resource.URI)
	switch {
	case err != nil:
		return fmt.Errorf("parse setup resource URI %q: %w", resource.URI, err)
	case parsed.Scheme != "gram" || parsed.Host != "platform-mcp" || !strings.HasPrefix(resource.URI, "gram://platform-mcp/setup/"):
		return fmt.Errorf("setup resource URI %q is not under gram://platform-mcp/setup/", resource.URI)
	case resource.URI != setupResourceURIFromURI(resource.URI):
		return fmt.Errorf("setup resource URI %q is not exactly gram://platform-mcp/setup/<provider>/<intent>", resource.URI)
	case resource.Name == "" || resource.Title == "" || resource.Description == "":
		return fmt.Errorf("setup resource %q is missing a name, title, or description", resource.URI)
	case resource.Text == "":
		return fmt.Errorf("setup resource %q has no content", resource.URI)
	case len(resource.Text) > maxSetupResourceBytes:
		return fmt.Errorf("setup resource %q is %d bytes, over the %d byte limit", resource.URI, len(resource.Text), maxSetupResourceBytes)
	case resource.Source == "" || resource.Owner == "":
		// A guide without a cited source and an accountable owner is exactly
		// the unattributed content this corpus exists to replace.
		return fmt.Errorf("setup resource %q is missing its owner or source", resource.URI)
	case resource.ObservedAt.IsZero() || resource.RevalidateBy.IsZero():
		return fmt.Errorf("setup resource %q is missing its observation or revalidation date", resource.URI)
	default:
		return nil
	}
}

// SetupResourceURI is the stable address of one reviewed guide. It is exported
// because a corpus builder must produce exactly these URIs — they are a public
// contract with every MCP client that has one bookmarked.
func SetupResourceURI(provider, intent string) string {
	return fmt.Sprintf("gram://platform-mcp/setup/%s/%s", provider, intent)
}

func setupResourceURIFromURI(uri string) string {
	parts := strings.Split(strings.TrimPrefix(uri, "gram://platform-mcp/setup/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return SetupResourceURI(parts[0], parts[1])
}
