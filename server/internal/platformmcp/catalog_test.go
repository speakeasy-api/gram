package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
)

func TestEntryHasAllowedStreamableHTTPRemote(t *testing.T) {
	t.Parallel()

	entry := &types.ExternalMCPServerEntry{
		Remotes: []*types.ExternalMCPRemote{
			{URL: "https://example.test/sse", TransportType: "sse"},
			{URL: "https://example.test/mcp", TransportType: "streamable-http"},
		},
	}

	require.True(t, entryHasAllowedStreamableHTTPRemote(entry, "https://example.test/mcp"))
	require.False(t, entryHasAllowedStreamableHTTPRemote(entry, "http://example.test/mcp"))
	require.False(t, entryHasAllowedStreamableHTTPRemote(entry, "https://user:password@example.test/mcp"))
	require.False(t, entryHasAllowedStreamableHTTPRemote(entry, "https://example.test/sse"))
	require.False(t, entryHasAllowedStreamableHTTPRemote(entry, "https://other.test/mcp"))
	require.False(t, entryHasAllowedStreamableHTTPRemote(entry, "https://example.test/{region}/mcp"))
}

func TestHTTPSURLRejectsUnsafeURLs(t *testing.T) {
	t.Parallel()

	require.True(t, isHTTPSURL("https://example.test/mcp"))
	require.False(t, isHTTPSURL("http://example.test/mcp"))
	require.False(t, isHTTPSURL("https://user:password@example.test/mcp"))
	require.False(t, isHTTPSURL("https://:443/mcp"))
	require.False(t, isHTTPSURL("https://example.test/mcp#fragment"))
	require.False(t, isHTTPSURL("javascript:alert(1)"))
}

func TestHasUnresolvedRemoteTemplate(t *testing.T) {
	t.Parallel()

	require.True(t, hasUnresolvedRemoteTemplate("https://example.test/{region}/mcp"))
	require.True(t, hasUnresolvedRemoteTemplate("https://example.test/mcp?tenant={tenant}"))
	require.False(t, hasUnresolvedRemoteTemplate("https://example.test/mcp"))
}

func TestCatalogDetailsUseSnakeCaseJSONKeys(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(CatalogDetails{CatalogCandidate: CatalogCandidate{
		ProviderKey: "provider", CatalogRef: "reviewed/mcp", ToolCount: 1, SetupIntent: "authorize",
	}, Transport: "streamable-http", ToolNames: []string{"tool"}})

	require.NoError(t, err)
	require.JSONEq(t, `{"provider_key":"provider","catalog_ref":"reviewed/mcp","name":"","description":"","version":"","tool_count":1,"setup_intent":"authorize","transport":"streamable-http","tool_names":["tool"]}`, string(encoded))
}

func TestCatalogCandidateFromEntryUsesConfiguredIdentity(t *testing.T) {
	t.Parallel()

	title := "Reviewed MCP"
	candidate := catalogCandidateFromEntry(CatalogDescriptor{
		ProviderKey:  "reviewed-provider",
		CanonicalRef: "reviewed/mcp",
		SetupIntent:  "authorize",
	}, &types.ExternalMCPServerEntry{
		RegistrySpecifier: "untrusted/entry",
		Title:             &title,
		Description:       "description",
		Version:           "1.2.3",
		ToolCount:         4,
	})

	require.Equal(t, "reviewed-provider", candidate.ProviderKey)
	require.Equal(t, "reviewed/mcp", candidate.CatalogRef)
	require.Equal(t, "Reviewed MCP", candidate.Name)
	require.Equal(t, "authorize", candidate.SetupIntent)
}
