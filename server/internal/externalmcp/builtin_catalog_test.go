package externalmcp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
)

func TestListBuiltInCatalogIncludesOfficialGoogleDriveAndDocs(t *testing.T) {
	t.Parallel()

	servers := listBuiltInCatalog("")
	require.Len(t, servers, 2)

	drive := servers[0]
	require.Equal(t, googleDriveRegistrySpecifier, drive.RegistrySpecifier)
	require.Equal(t, "Google Drive", *drive.Title)
	require.Equal(t, "https://drivemcp.googleapis.com/mcp/v1", drive.Remotes[0].URL)
	require.Equal(t, 8, drive.ToolCount)
	require.False(t, drive.IsReadOnly)
	require.False(t, drive.SupportsDcr)
	require.Equal(t, builtInRegistryID.String(), *drive.RegistryID)

	docs := servers[1]
	require.Equal(t, googleDocsRegistrySpecifier, docs.RegistrySpecifier)
	require.Equal(t, "Google Docs", *docs.Title)
	require.Equal(t, "https://docsmcp.googleapis.com/mcp/v1", docs.Remotes[0].URL)
	require.Equal(t, 2, docs.ToolCount)
	require.False(t, docs.IsReadOnly)
	require.False(t, docs.SupportsDcr)
}

func TestListBuiltInCatalogFiltersBySearch(t *testing.T) {
	t.Parallel()

	servers := listBuiltInCatalog("richly editing")
	require.Len(t, servers, 1)
	require.Equal(t, googleDocsRegistrySpecifier, servers[0].RegistrySpecifier)
}

func TestGetBuiltInCatalogDetailsIncludesToolsAndSetupMetadata(t *testing.T) {
	t.Parallel()

	details := getBuiltInCatalogDetails(googleDocsRegistrySpecifier)
	require.NotNil(t, details)
	require.Equal(t, []string{"read_doc", "update_doc"}, toolNames(details.Tools))

	meta, ok := details.Meta.(map[string]any)
	require.True(t, ok)
	setup, ok := meta["com.getgram/catalog"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "customer", setup["oauthClientOwnership"])
	require.Equal(t, []string{"docs.googleapis.com"}, setup["requiredApis"])
	require.Equal(t, []string{"docsmcp.googleapis.com"}, setup["requiredMcpServices"])
	require.Equal(t, []string{
		"https://www.googleapis.com/auth/drive.readonly",
		"https://www.googleapis.com/auth/drive.file",
		"https://www.googleapis.com/auth/documents.readonly",
		"https://www.googleapis.com/auth/documents",
	}, setup["requiredScopes"])
}

func TestMergeBuiltInCatalogPrefersGramDefinitions(t *testing.T) {
	t.Parallel()

	duplicate := &types.ExternalMCPServerEntry{
		RegistrySpecifier:                   googleDriveRegistrySpecifier,
		Version:                             "9.9.9",
		Description:                         "registry duplicate",
		ToolsetID:                           nil,
		McpServerID:                         nil,
		RegistryID:                          nil,
		OrganizationMcpCollectionRegistryID: nil,
		Title:                               nil,
		IconURL:                             nil,
		Meta:                                nil,
		ToolCount:                           0,
		IsReadOnly:                          true,
		SupportsDcr:                         true,
		Remotes:                             nil,
	}
	other := &types.ExternalMCPServerEntry{
		RegistrySpecifier:                   "example/other",
		Version:                             "1.0.0",
		Description:                         "other",
		ToolsetID:                           nil,
		McpServerID:                         nil,
		RegistryID:                          nil,
		OrganizationMcpCollectionRegistryID: nil,
		Title:                               nil,
		IconURL:                             nil,
		Meta:                                nil,
		ToolCount:                           0,
		IsReadOnly:                          false,
		SupportsDcr:                         false,
		Remotes:                             nil,
	}

	servers := mergeBuiltInCatalog([]*types.ExternalMCPServerEntry{duplicate, other}, "")
	require.Len(t, servers, 3)
	require.Equal(t, "1.0.0", servers[0].Version)
	require.Equal(t, googleDocsRegistrySpecifier, servers[1].RegistrySpecifier)
	require.Equal(t, "example/other", servers[2].RegistrySpecifier)
}

func toolNames(tools []*types.ExternalMCPTool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, *tool.Name)
	}
	return names
}
