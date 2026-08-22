package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
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
	require.JSONEq(t, `{"provider_key":"provider","catalog_ref":"reviewed/mcp","name":"","description":"","version":"","tool_count":1,"setup_intent":"authorize","transport":"streamable-http","tool_names":["tool"],"configuration":null,"requires_dashboard_setup":false}`, string(encoded))
}

func TestCatalogConfigurationRejectsSecretAndUndeclaredValues(t *testing.T) {
	t.Parallel()

	details := CatalogDetails{
		Configuration: []CatalogConfigurationField{
			{Key: "header:x-label", Kind: "header", Name: "X-Label", Required: true},
			{Key: "header:x-api-key", Kind: "header", Name: "X-API-Key", Required: true, Secret: true},
			{Key: "url_variable:region", Kind: "url_variable", Name: "region", Required: true, Choices: []string{"us", "eu"}},
		},
		remoteURLTemplate: "https://example.test/{region}/mcp",
	}

	_, err := details.resolveConfiguration(CatalogConfigurationValues{
		"header:x-label":      "label",
		"header:x-api-key":    "never-accepted",
		"url_variable:region": "us",
	})
	require.ErrorIs(t, err, ErrCatalogConfigurationRejected)

	_, err = details.resolveConfiguration(CatalogConfigurationValues{
		"header:x-label":       "label",
		"url_variable:unknown": "value",
		"url_variable:region":  "us",
	})
	require.ErrorIs(t, err, ErrCatalogConfigurationRejected)

	resolved, err := details.resolveConfiguration(CatalogConfigurationValues{
		"header:x-label":      "label",
		"url_variable:region": "eu",
	})
	require.NoError(t, err)
	require.Equal(t, "https://example.test/eu/mcp", resolved.remoteURL)
	require.Equal(t, []resolvedCatalogHeader{
		{name: "X-Label", required: true, value: "label"},
		{name: "X-API-Key", required: true, secret: true},
	}, resolved.headers)
	require.Equal(t, []CatalogConfigurationField{{Key: "header:x-api-key", Kind: "header", Name: "X-API-Key", Required: true, Secret: true}}, resolved.pendingSecretFields)
}

func TestCatalogConfigurationRejectsSecretURLVariables(t *testing.T) {
	t.Parallel()

	_, err := (CatalogDetails{
		Configuration: []CatalogConfigurationField{{
			Key: "url_variable:tenant", Kind: "url_variable", Name: "tenant", Required: true, Secret: true,
		}},
		remoteURLTemplate: "https://example.test/{tenant}/mcp",
	}).resolveConfiguration(nil)

	require.ErrorIs(t, err, ErrCatalogConfigurationRejected)
}

func TestCatalogConfigurationHashesAreDeterministicAndDistinct(t *testing.T) {
	t.Parallel()

	first := CatalogConfigurationValues{
		"header:x-label":      "label",
		"url_variable:region": "eu",
	}
	second := CatalogConfigurationValues{
		"url_variable:region": "eu",
		"header:x-label":      "label",
	}

	firstHash := catalogConfigurationHash(first)
	require.NotEmpty(t, firstHash)
	require.Equal(t, firstHash, catalogConfigurationHash(second))
	require.NotEqual(t, firstHash, catalogConfigurationHash(CatalogConfigurationValues{
		"header:x-label":      "other-label",
		"url_variable:region": "eu",
	}))
	require.Empty(t, catalogConfigurationHash(nil))

	registrationHash := catalogRegistrationInputHash("project", "catalog", "provider", "reviewed/mcp", firstHash)
	require.NotEqual(t, registrationHash, catalogRegistrationInputHash("project", "catalog", "provider", "reviewed/mcp", catalogConfigurationHash(CatalogConfigurationValues{
		"header:x-label":      "other-label",
		"url_variable:region": "eu",
	})))
}

func TestBrowserCatalogProviderKeyRequiresValidRegistryUUID(t *testing.T) {
	t.Parallel()

	require.True(t, isBrowserCatalogProviderKey("browser-catalog-registry-7e966bfa-4df0-43ef-a54c-9c8c2e5f1b0d"))
	require.False(t, isBrowserCatalogProviderKey("browser-catalog-registry-not-a-uuid"))
	require.False(t, isBrowserCatalogProviderKey("provider-7e966bfa-4df0-43ef-a54c-9c8c2e5f1b0d"))
}

// Browser-catalogue entries and remote URL registrations continue setup on the
// Remote MCP Authentication settings dashboard page; only fixture adapter
// registrations take the catalogue setup handoff. A remote URL registration
// misrouted into the handoff path would fail its catalogue inspection, so the
// dashboard onboarding continuation depends on this routing.
func TestRegistrationUsesDashboardSetupRoutesRemoteURLAndBrowserCatalog(t *testing.T) {
	t.Parallel()

	require.True(t, registrationUsesDashboardSetup(remoteURLCatalogProvider))
	require.True(t, registrationUsesDashboardSetup("browser-catalog-registry-7e966bfa-4df0-43ef-a54c-9c8c2e5f1b0d"))
	require.False(t, registrationUsesDashboardSetup("fixture-provider"))
	require.False(t, registrationUsesDashboardSetup(""))
}

func TestBrowserCatalogDescriptorUsesRegistryScopedOpaqueIdentity(t *testing.T) {
	t.Parallel()

	registry := externalmcp.Registry{ID: uuid.MustParse("7e966bfa-4df0-43ef-a54c-9c8c2e5f1b0d"), URL: "https://catalogue.example.test"}
	descriptor := BrowserCatalogDescriptor(registry)

	require.Equal(t, registry, descriptor.Registry)
	require.Equal(t, "browser-catalog-registry-7e966bfa-4df0-43ef-a54c-9c8c2e5f1b0d", descriptor.ProviderKey)
	require.Equal(t, "dashboard_source_settings", descriptor.SetupIntent)
	require.Empty(t, descriptor.CanonicalRef)
	require.Empty(t, descriptor.AllowedRemoteURL)
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
	require.Equal(t, "untrusted/entry", candidate.CatalogRef)
	require.Equal(t, "Reviewed MCP", candidate.Name)
	require.Equal(t, "authorize", candidate.SetupIntent)
}
