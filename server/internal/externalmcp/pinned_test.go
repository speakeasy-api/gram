package externalmcp

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
)

func TestIsPulseCatalogRegistry(t *testing.T) {
	t.Parallel()

	require.True(t, isPulseCatalogRegistry("https://api.pulsemcp.com"))
	require.True(t, isPulseCatalogRegistry("https://api.pulsemcp.com/"))
	require.False(t, isPulseCatalogRegistry("https://api.pulsemcp.com.evil.test"))
	require.False(t, isPulseCatalogRegistry("http://127.0.0.1:9"))
	require.False(t, isPulseCatalogRegistry("not a url"))
}

func TestMergePinnedServersAddsMissingSpecifier(t *testing.T) {
	t.Parallel()

	existing := []*types.ExternalMCPServerEntry{
		listEntry("com.pulsemcp.mirror/github"),
	}
	fetched := 0
	result := mergePinnedServers(existing, func(specifier string) (*types.ExternalMCPServerEntry, error) {
		fetched++
		return listEntry(specifier), nil
	})

	require.Equal(t, 1, fetched)
	require.Equal(t, []string{
		"com.pulsemcp.mirror/github",
		"com.pulsemcp.mirror/salesforce-platform",
	}, registrySpecifiers(result))
}

func TestMergePinnedServersSkipsSpecifierAlreadyPresent(t *testing.T) {
	t.Parallel()

	existing := []*types.ExternalMCPServerEntry{
		listEntry("com.pulsemcp.mirror/salesforce-platform"),
	}
	result := mergePinnedServers(existing, func(string) (*types.ExternalMCPServerEntry, error) {
		t.Fatal("fetch must not run when the pin is already listed")
		return nil, nil
	})

	require.Equal(t, existing, result)
}

func TestMergePinnedServersIgnoresFetchErrors(t *testing.T) {
	t.Parallel()

	existing := []*types.ExternalMCPServerEntry{
		listEntry("com.pulsemcp.mirror/github"),
	}
	result := mergePinnedServers(existing, func(string) (*types.ExternalMCPServerEntry, error) {
		return nil, fmt.Errorf("registry unavailable")
	})

	require.Equal(t, existing, result)
}

func TestMergePinnedServersIgnoresMismatchedSpecifier(t *testing.T) {
	t.Parallel()

	existing := []*types.ExternalMCPServerEntry{
		listEntry("com.pulsemcp.mirror/github"),
	}
	result := mergePinnedServers(existing, func(string) (*types.ExternalMCPServerEntry, error) {
		return listEntry("com.pulsemcp.mirror/someone-else"), nil
	})

	require.Equal(t, existing, result)
}

func TestCapCatalogServersPreservesPinnedOverflow(t *testing.T) {
	t.Parallel()

	servers := make([]*types.ExternalMCPServerEntry, 0, catalogListCap+1)
	for i := range catalogListCap {
		servers = append(servers, listEntry(fmt.Sprintf("com.pulsemcp.mirror/server-%02d", i)))
	}
	servers = append(servers, listEntry("com.pulsemcp.mirror/salesforce-platform"))

	capped := capCatalogServers(servers, catalogListCap)
	require.Len(t, capped, catalogListCap)
	require.Contains(t, registrySpecifiers(capped), "com.pulsemcp.mirror/salesforce-platform")
	require.NotContains(t, registrySpecifiers(capped), "com.pulsemcp.mirror/server-99")
}

func TestCapCatalogServersLeavesShortListsUnchanged(t *testing.T) {
	t.Parallel()

	servers := []*types.ExternalMCPServerEntry{
		listEntry("com.pulsemcp.mirror/github"),
		listEntry("com.pulsemcp.mirror/salesforce-platform"),
	}
	require.Equal(t, servers, capCatalogServers(servers, catalogListCap))
}

func TestIsPinnedRegistrySpecifier(t *testing.T) {
	t.Parallel()

	require.True(t, isPinnedRegistrySpecifier("com.pulsemcp.mirror/salesforce-platform"))
	require.False(t, isPinnedRegistrySpecifier("com.pulsemcp.mirror/github"))
}

func listEntry(specifier string) *types.ExternalMCPServerEntry {
	return &types.ExternalMCPServerEntry{
		RegistrySpecifier:                   specifier,
		Version:                             "1.0.0",
		Description:                         specifier,
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
		Repository:                          nil,
		Packages:                            nil,
	}
}
