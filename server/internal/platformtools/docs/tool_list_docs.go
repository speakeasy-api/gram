package docs

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// maxListResults caps a single index response. The live corpus is ~110 pages,
// so an unfiltered call returns everything today; the cap only guards against
// the docs site growing enough to crowd the model's context, and truncation is
// always reported rather than applied silently.
const maxListResults = 200

// ListDocs returns the AI control plane documentation index so the managed
// assistant can choose a page to read with platform_get_doc.
type ListDocs struct {
	client *Client
}

type listDocsInput struct {
	Query   string `json:"query,omitempty" jsonschema:"Case-insensitive substring matched against each page's title and path. Omit to list every page — the full index is small enough to scan."`
	Section string `json:"section,omitempty" jsonschema:"Restrict results to one top-level section, such as connect, distribute, observe, guides, or org-admin. Omit to search all sections."`
}

type listDocsResult struct {
	Pages     []Page   `json:"pages"`
	Total     int      `json:"total"`
	Truncated bool     `json:"truncated,omitempty"`
	Sections  []string `json:"sections"`
	SourceURL string   `json:"source_url"`
}

// NewListDocsTool builds the docs index tool over client. Share one client
// with the page-fetching tool so the index is fetched and cached once.
func NewListDocsTool(client *Client) *ListDocs {
	return &ListDocs{client: client}
}

func (s *ListDocs) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "docs",
		HandlerName: "list_docs",
		Name:        "platform_list_docs",
		Description: "List the Speakeasy AI control plane product documentation pages (speakeasy.com/docs/ai-control-plane). Use this first to find the right page, then read it with platform_get_doc. Each result has a path, title, section, and permalink. Filter with query and/or section, or call with no arguments to see the whole index.",
		InputSchema: core.BuildInputSchema[listDocsInput](),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *ListDocs) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	input := listDocsInput{Query: "", Section: ""}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}

	pages, err := s.client.Index(ctx)
	if err != nil {
		return fmt.Errorf("load docs index: %w", err)
	}

	sections := sectionsOf(pages)

	// Validate the section against the live index rather than a hardcoded enum
	// so a section added to the docs site is usable without a server release,
	// and a typo comes back with the real options instead of empty results.
	section := strings.ToLower(strings.TrimSpace(input.Section))
	if section != "" && !slices.Contains(sections, section) {
		return fmt.Errorf("unknown section %q: must be one of %s", input.Section, strings.Join(sections, ", "))
	}

	query := strings.ToLower(strings.TrimSpace(input.Query))
	matched := make([]Page, 0, len(pages))
	for _, page := range pages {
		if section != "" && page.Section != section {
			continue
		}
		if query != "" &&
			!strings.Contains(strings.ToLower(page.Title), query) &&
			!strings.Contains(strings.ToLower(page.Path), query) {
			continue
		}
		matched = append(matched, page)
	}

	out := matched
	truncated := false
	if len(out) > maxListResults {
		out = out[:maxListResults]
		truncated = true
	}

	return core.EncodeResult(wr, listDocsResult{
		Pages:     out,
		Total:     len(matched),
		Truncated: truncated,
		Sections:  sections,
		SourceURL: s.client.SiteURL() + PathPrefix,
	})
}

// sectionsOf returns the distinct top-level sections present in the index,
// sorted, so a caller that guessed wrong can see the real options.
func sectionsOf(pages []Page) []string {
	seen := make(map[string]bool, len(pages))
	sections := make([]string, 0, len(pages))
	for _, page := range pages {
		if page.Section == "" || seen[page.Section] {
			continue
		}
		seen[page.Section] = true
		sections = append(sections, page.Section)
	}
	sort.Strings(sections)
	return sections
}
