package docs

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// GetDoc returns the markdown body of one AI control plane documentation page.
type GetDoc struct {
	client *Client
}

type getDocInput struct {
	Path string `json:"path" jsonschema:"Page path as returned by platform_list_docs, for example /docs/ai-control-plane/observe/tool-logs. A full speakeasy.com URL is also accepted. Only pages under /docs/ai-control-plane can be read."`
}

type getDocResult struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Section string `json:"section,omitempty"`
	URL     string `json:"url"`
	Content string `json:"content"`
	// ChildPages is populated only for section landing pages whose markdown is
	// a stub, so the model gets somewhere to go instead of an empty body.
	ChildPages []Page `json:"child_pages,omitempty"`
	Note       string `json:"note,omitempty"`
}

// NewGetDocTool builds the page-fetching tool over client. Share one client
// with the listing tool so the index is fetched and cached once.
func NewGetDocTool(client *Client) *GetDoc {
	return &GetDoc{client: client}
}

func (s *GetDoc) Descriptor() core.ToolDescriptor {
	return core.ToolDescriptor{
		SourceSlug:  "docs",
		HandlerName: "get_doc",
		Name:        "platform_get_doc",
		Description: "Read one Speakeasy AI control plane documentation page as markdown. Find the path with platform_list_docs first. Returns the page body plus its public permalink — cite that permalink when answering from the docs.",
		InputSchema: core.BuildInputSchema[getDocInput](),
		Variables:   nil,
		Annotations: core.ReadOnlyAnnotations(),
		Managed:     true,
		OwnerKind:   nil,
		OwnerID:     nil,
	}
}

func (s *GetDoc) Call(ctx context.Context, _ toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	input := getDocInput{Path: ""}
	if err := core.DecodeInput(payload, &input); err != nil {
		return err
	}

	path := canonicalizePath(input.Path)
	if path == "" {
		return fmt.Errorf("path is required: pass a page path from platform_list_docs")
	}

	pages, err := s.client.Index(ctx)
	if err != nil {
		return fmt.Errorf("load docs index: %w", err)
	}

	// Serving only indexed paths is what keeps this a documentation tool
	// rather than a general fetcher: an unlisted or off-site path is refused
	// before any request is made.
	var page Page
	found := false
	for _, candidate := range pages {
		if candidate.Path == path {
			page = candidate
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown documentation page %q: use platform_list_docs to find a valid path under %s", input.Path, PathPrefix)
	}

	content, err := s.client.Content(ctx, page.Path)
	if err != nil {
		return fmt.Errorf("read docs page: %w", err)
	}

	result := getDocResult{
		Path:       page.Path,
		Title:      page.Title,
		Section:    page.Section,
		URL:        page.URL,
		Content:    content,
		ChildPages: nil,
		Note:       "",
	}

	// Section landing pages render their child links client-side, so their
	// markdown counterpart is close to empty. Hand back the section's pages
	// rather than letting an empty body read as "no documentation exists".
	if len(content) < minMeaningfulPageLen {
		if children := childrenOf(pages, page.Path); len(children) > 0 {
			result.ChildPages = children
			result.Note = "This is a section landing page with no substantive markdown body. The pages listed in child_pages hold the content for this section."
		}
	}

	return core.EncodeResult(wr, result)
}

// canonicalizePath accepts the several shapes a model may produce — a bare
// path, a path with a trailing slash, the ".md" URL the page is fetched from,
// or a full permalink — and reduces them to the indexed form. Being lenient
// here avoids a retry loop over what is only a formatting difference.
func canonicalizePath(raw string) string {
	path := strings.TrimSpace(raw)
	if path == "" {
		return ""
	}

	if idx := strings.Index(path, "://"); idx >= 0 {
		rest := path[idx+len("://"):]
		slash := strings.Index(rest, "/")
		if slash < 0 {
			return ""
		}
		path = rest[slash:]
	}

	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".md")
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return path
}

// childrenOf returns the pages directly beneath parent, nearest level only, so
// a stub landing page points at its own section rather than the whole tree.
func childrenOf(pages []Page, parent string) []Page {
	prefix := parent + "/"
	children := make([]Page, 0, len(pages))
	for _, page := range pages {
		rest := strings.TrimPrefix(page.Path, prefix)
		if rest == page.Path || rest == "" || strings.Contains(rest, "/") {
			continue
		}
		children = append(children, page)
	}
	return children
}
