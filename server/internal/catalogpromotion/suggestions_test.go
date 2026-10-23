package catalogpromotion

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion/repo"
	"github.com/stretchr/testify/require"
)

func TestSuggestedMetadataUsesObservedResource(t *testing.T) {
	t.Parallel()
	result := suggestedMetadata(repo.GetSourceRow{SourceName: "Private deployment label", RemoteName: "Example", ResourceName: "Example MCP", Url: "https://mcp.example.com/mcp", ResourceDocumentation: "https://docs.example.com/setup", RemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, RequiresUpstreamAuth: true})
	require.Equal(t, "Example MCP", result.Title)
	require.Equal(t, "com.example.mcp/example-mcp", result.Name)
	require.Equal(t, "example-mcp", result.DocsSlug)
	require.Equal(t, "https://mcp.example.com/mcp", result.URL)
	require.Equal(t, "https://docs.example.com/setup", result.DocumentationURL)
	require.Equal(t, "oauth", result.AuthMode)
	require.Empty(t, result.Description)
}

func TestSuggestedMetadataOmitsUnsafeEndpoints(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []string{"https://mcp.example.com/mcp?token=EXAMPLE", "https://user:EXAMPLE@mcp.example.com/mcp", "https://mcp.example.com/mcp#EXAMPLE", "https://mcp.example.com/private/EXAMPLE", "https://mcp.example.com/%6dcp", "http://mcp.example.com/mcp", "https://127.0.0.1/mcp"} {
		t.Run(endpoint, func(t *testing.T) {
			t.Parallel()
			result := suggestedMetadata(repo.GetSourceRow{RemoteName: "Example", Url: endpoint, ResourceDocumentation: "https://docs.example.com/setup?token=EXAMPLE", RequiresUpstreamAuth: true})
			require.Empty(t, result.URL)
			require.Empty(t, result.Name)
			require.Empty(t, result.DocsSlug)
			require.Empty(t, result.DocumentationURL)
			require.Empty(t, result.AuthMode)
		})
	}
}

func TestSuggestedMetadataBoundsDraftsAndDoesNotInventDocs(t *testing.T) {
	t.Parallel()
	result := suggestedMetadata(repo.GetSourceRow{UpstreamAuthKnown: true, RemoteName: strings.Repeat("é", 100000), Url: "https://mcp.example.com/mcp"})
	require.LessOrEqual(t, len(result.Title), 200)
	require.True(t, utf8.ValidString(result.Title))
	require.Empty(t, result.Name)
	require.Empty(t, result.Description)
	require.Empty(t, result.DocumentationURL)
	require.Empty(t, result.DocsSlug)
	require.Equal(t, "none", result.AuthMode)
}

func TestSuggestedMetadataLeavesUnknownAuthenticationUnset(t *testing.T) {
	t.Parallel()
	result := suggestedMetadata(repo.GetSourceRow{RemoteName: "Example", Url: "https://mcp.example.com/mcp"})
	require.Empty(t, result.AuthMode)
}

func TestSuggestedMetadataNormalizesTitleFallbacks(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, resource, remote, source, want string }{
		{name: "resource whitespace", resource: " \t\n", remote: " Example  MCP ", source: "Fallback", want: "Example MCP"},
		{name: "remote whitespace", resource: " ", remote: "\t", source: " Example Source ", want: "Example Source"},
		{name: "truncated space", resource: strings.Repeat("a", 199) + " more", want: strings.Repeat("a", 199)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := suggestedMetadata(repo.GetSourceRow{ResourceName: test.resource, RemoteName: test.remote, SourceName: test.source})
			require.Equal(t, test.want, result.Title)
		})
	}
}

func TestSuggestedMetadataNormalizesTrailingDotNamespace(t *testing.T) {
	t.Parallel()
	result := suggestedMetadata(repo.GetSourceRow{ResourceName: "Example MCP", Url: "https://mcp.example.com./mcp"})
	require.Equal(t, "com.example.mcp/example-mcp", result.Name)
	require.Equal(t, "https://mcp.example.com./mcp", result.URL)
}
