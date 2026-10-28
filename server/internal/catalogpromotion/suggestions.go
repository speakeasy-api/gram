package catalogpromotion

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/catalogpromotion/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/setupguides"
)

// suggestedMetadata supplies editable staff-only drafts from observed metadata.
// It neither establishes public suitability nor substitutes for staff confirmation.
func suggestedMetadata(source repo.GetSourceRow) *PublicMetadata {
	title := ""
	for _, candidate := range []string{source.ResourceName, source.RemoteName, source.SourceName} {
		if normalized := strings.Join(strings.Fields(candidate), " "); normalized != "" {
			title = normalized
			break
		}
	}
	title = conv.TruncateString(title, maxPublicDisplayCharacters)
	title = strings.TrimSpace(title)
	result := &PublicMetadata{Name: "", Title: title, Description: "", URL: "", DocumentationURL: "", DocsSlug: "", AuthMode: ""}
	if source.RemoteSessionIssuerID.Valid {
		result.AuthMode = "oauth"
	} else if source.UpstreamAuthKnown && !source.RequiresUpstreamAuth {
		result.AuthMode = "none"
	}
	parsed, err := publicURL(source.Url)
	if err == nil {
		// Arbitrary path segments can contain private tenant IDs or bearer tokens.
		// Only conventional shared endpoints are suggested; do not strip a URL into
		// a different endpoint when any source component is unsafe.
		switch parsed.EscapedPath() {
		case "", "/", "/mcp", "/mcp/", "/sse", "/sse/", "/api/mcp", "/api/mcp/", "/v1/mcp", "/v1/mcp/", "/v1/sse", "/v1/sse/":
			result.URL = parsed.String()
			parts := strings.Split(strings.TrimSuffix(parsed.Hostname(), "."), ".")
			for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
				parts[left], parts[right] = parts[right], parts[left]
			}
			slug := conv.URLToSlug(title)
			if len(slug) > 80 {
				slug = strings.TrimRight(slug[:80], "-")
			}
			if slug != "" {
				result.DocsSlug = slug
				name := strings.Join(parts, ".") + "/" + slug
				if len(name) <= 200 {
					result.Name = name
				}
			}
			// A guide that already documents this exact endpoint is the right
			// guide, whatever the source is called, so reuse its slug rather than
			// requesting a duplicate under a title-derived one.
			if guide, ok := setupguides.ForEndpoint(result.URL); ok {
				result.DocsSlug = guide.Slug
			}
		}
	}
	if documentation, err := publicURL(source.ResourceDocumentation); err == nil {
		result.DocumentationURL = documentation.String()
	}
	return result
}
