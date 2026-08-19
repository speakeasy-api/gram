//nolint:exhaustruct // MCP SDK manifests intentionally rely on documented zero-value optional fields.
package platformmcp

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ReadGramDocToolInput struct {
	URI string `json:"uri" jsonschema:"gram:// resource URI of a reviewed guide, exactly as returned by search_gram_docs, for example gram://platform-mcp/setup/github/provider_setup"`
}

type ReadGramDocToolOutput struct {
	URI   string `json:"uri"`
	Title string `json:"title,omitempty"`
	// Text is the full reviewed guide, including its own citation header.
	Text    string `json:"text,omitempty"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// registerReadDocTool gives the assistant a way to follow a citation.
//
// An external MCP client reads a cited guide with resources/read. The assistant
// speaks Go rather than MCP and its tool channel has no resource methods, so
// without this the gram:// URIs in a search result would resolve on one surface
// and dangle on the other — a citation that looks openable and is not. The tool
// reads the same registered resources, through the same freshness rules, so
// both surfaces answer identically.
func registerReadDocTool(reg *Registrar) {
	addTool(reg, &mcp.Tool{
		Name:        "read_gram_doc",
		Title:       "Read Gram Doc",
		Description: "Read one reviewed Speakeasy AICP guide in full by its gram:// resource URI, as returned by search_gram_docs. Returns guide_unavailable when no reviewed guide stands behind that URI: say so and hand the user the canonical links rather than inventing steps.",
		Annotations: readOnlyAnnotations(),
	}, ToolMeta{
		// Assistant-only. External clients read the same content through
		// resources/read, and offering both would be two names for one thing.
		Audiences: []Audience{AudienceAssistant}, ProjectScope: ProjectScopeNone,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ReadGramDocToolInput) (*mcp.CallToolResult, ReadGramDocToolOutput, error) {
		if _, err := principalFromToolContext(ctx); err != nil {
			return nil, ReadGramDocToolOutput{}, err
		}
		resource, ok := reg.ResourceFor(AudienceAssistant, input.URI)
		if !ok {
			return nil, docUnavailable(input.URI, "No reviewed guide is published at this URI. Do not reconstruct the guide: tell the user it is not covered and point them at the provider's own documentation."), nil
		}
		text, err := resource.Read(ctx)
		if errors.Is(err, ErrSetupGuideUnavailable) {
			return nil, docUnavailable(input.URI, "This guide is too far past its revalidation date to stand behind and has been withheld. Tell the user the reviewed guide is stale and point them at the provider's own documentation."), nil
		}
		if err != nil {
			return nil, ReadGramDocToolOutput{}, err
		}
		return nil, ReadGramDocToolOutput{URI: resource.URI, Title: resource.Title, Text: text}, nil
	})
}

func docUnavailable(uri, message string) ReadGramDocToolOutput {
	return ReadGramDocToolOutput{URI: uri, Code: setupGuideUnavailableCode, Message: message}
}
