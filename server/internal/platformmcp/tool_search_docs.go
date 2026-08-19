//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchGramDocsToolInput struct {
	Query string `json:"query" jsonschema:"question or provider name to look up in the reviewed Gram documentation corpus, for example 'GitHub OAuth client id' or 'Snowflake tenant URL'"`
}

type SearchGramDocsToolOutput struct {
	Query string `json:"query"`
	// Excerpts is empty when nothing reviewed answers the query. It is never
	// filled with a paraphrase or a guess.
	Excerpts []DocsExcerpt `json:"excerpts"`
	// Code is set only when no excerpt could be returned, so a caller can tell
	// "the corpus has no answer" from "the corpus answered".
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

const searchGramDocsDescription = "Search the reviewed Speakeasy AICP setup and documentation corpus and return up to five cited excerpts. The corpus is a pinned, reviewed export: this tool never reads the live web, a provider's pages, or unreviewed search results. Every excerpt carries its source, version, observation date, and canonical links, plus the gram:// resource URI of the full guide — read that resource for the complete steps. If the result is guide_unavailable or an excerpt is marked stale, tell the user what is missing and hand them the canonical links. Never invent setup steps that an excerpt does not state."

func registerSearchDocsTool(reg *Registrar, index DocsIndex, budget OperationBudget) {
	addTool(reg, &mcp.Tool{
		Name:        "search_gram_docs",
		Title:       "Search Gram Docs",
		Description: searchGramDocsDescription,
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{
		// Both audiences, no project scope: the corpus is reviewed content for
		// the whole organization, and reading it touches no project state.
		Audiences: bothAudiences, ProjectScope: ProjectScopeNone,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SearchGramDocsToolInput) (*mcp.CallToolResult, SearchGramDocsToolOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, SearchGramDocsToolOutput{}, err
		}
		if err := budget.Allow(ctx, principal); err != nil {
			if budgetResult, ok := operationBudgetToolResult(err); ok {
				return budgetResult, SearchGramDocsToolOutput{}, nil
			}
			return nil, SearchGramDocsToolOutput{}, err
		}
		excerpts, err := index.Search(ctx, input.Query, maxDocsExcerpts)
		if err != nil {
			return nil, SearchGramDocsToolOutput{}, fmt.Errorf("search platform mcp docs corpus: %w", err)
		}
		if len(excerpts) == 0 {
			return nil, SearchGramDocsToolOutput{
				Query:    input.Query,
				Excerpts: []DocsExcerpt{},
				Code:     setupGuideUnavailableCode,
				Message:  "No reviewed documentation answers this query. Do not invent setup steps: tell the user no reviewed guide covers this yet, and point them at the provider's own documentation.",
			}, nil
		}
		return &mcp.CallToolResult{Content: docsExcerptContent(excerpts)}, SearchGramDocsToolOutput{
			Query:    input.Query,
			Excerpts: excerpts,
		}, nil
	})
}

// docsExcerptContent pairs each excerpt with a link to the resource it came
// from. The link is what makes a citation followable rather than decorative: a
// client that renders resource links lets the reader open the whole reviewed
// guide, and a client that does not still receives the excerpt text.
func docsExcerptContent(excerpts []DocsExcerpt) []mcp.Content {
	content := make([]mcp.Content, 0, len(excerpts)*2)
	for _, excerpt := range excerpts {
		content = append(content, &mcp.TextContent{Text: excerptCitation(excerpt)})
		content = append(content, &mcp.ResourceLink{
			URI:         excerpt.URI,
			Name:        excerpt.URI,
			Title:       excerpt.Title,
			Description: excerptLinkDescription(excerpt),
			MIMEType:    "text/markdown",
		})
	}
	return content
}

func excerptCitation(excerpt DocsExcerpt) string {
	header := excerpt.Title
	if excerpt.Heading != "" {
		header += " — " + excerpt.Heading
	}
	var citation strings.Builder
	fmt.Fprintf(&citation, "## %s\n\n%s\n\nSource: %s", header, excerpt.Excerpt, excerpt.Source)
	if excerpt.ObservedAt != "" {
		fmt.Fprintf(&citation, " (observed %s)", excerpt.ObservedAt)
	}
	if excerpt.Stale {
		citation.WriteString("\n\nThis guide is past its revalidation date. Verify it against the canonical links before following it.")
	}
	for _, link := range excerpt.Links {
		citation.WriteString("\n- ")
		citation.WriteString(link)
	}
	return citation.String()
}

func excerptLinkDescription(excerpt DocsExcerpt) string {
	if excerpt.Heading == "" {
		return "Full reviewed setup guide."
	}
	return fmt.Sprintf("Full reviewed setup guide containing %q.", excerpt.Heading)
}

func registerUnavailableSearchDocsTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "search_gram_docs",
		Title:       "Search Gram Docs",
		Description: "Search the reviewed Speakeasy AICP documentation corpus. Documentation search is not enabled in the current rollout.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{Audiences: bothAudiences, ProjectScope: ProjectScopeNone}, unavailableTool("docs_search"))
}
