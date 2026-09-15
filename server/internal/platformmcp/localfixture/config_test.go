package localfixture

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewConfigBuildsCodeOwnedCatalogDescriptor(t *testing.T) {
	t.Parallel()

	origin, err := url.Parse("https://localhost:8080")
	require.NoError(t, err)

	config, err := NewConfig(origin)
	require.NoError(t, err)
	descriptor := config.CatalogDescriptor()
	resources := config.SetupResources()

	require.Equal(t, ProviderKey, descriptor.ProviderKey)
	require.Equal(t, CanonicalRef, descriptor.CanonicalRef)
	require.Len(t, resources, 1)
	require.Equal(t, "gram://platform-mcp/setup/local-fixture/provider_setup", resources[0].URI)
	require.LessOrEqual(t, len(resources[0].Text), 32*1024)
	require.NotContains(t, resources[0].Text, config.RemoteURL())
	require.Equal(t, SetupIntent, descriptor.SetupIntent)
	require.Equal(t, "https://localhost:8080", descriptor.Registry.URL)
	require.Equal(t, "/v0.1/servers/local-fixture%2Freviewed-mcp/versions/latest", config.RegistryDetailsPath())
	require.Equal(t, "https://localhost:8080/platform-mcp/local-fixture/mcp", descriptor.AllowedRemoteURL)
	require.NotZero(t, descriptor.Registry.ID)
	require.NotZero(t, config.RemoteSessionIssuerID())
}

func TestNewConfigRejectsNonOriginURLs(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"http://localhost:8080",
		"https://user@localhost:8080",
		"https://localhost:8080/path",
		"https://localhost:8080/?query=value",
		"https://localhost:8080/#fragment",
		"https://:443",
		"https://localhost:8080?",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			origin, err := url.Parse(raw)
			require.NoError(t, err)
			config, err := NewConfig(origin)
			require.ErrorContains(t, err, "requires an HTTPS origin")
			require.Nil(t, config)
		})
	}
}
