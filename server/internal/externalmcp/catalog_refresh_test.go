package externalmcp_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
)

type retainedCatalogRefreshSource struct{ bundle mcpregistry.CatalogBundle }

func (s *retainedCatalogRefreshSource) Fetch(context.Context) (mcpregistry.CatalogBundle, error) {
	return s.bundle, nil
}

func TestCatalogRefreshRetainsInstalledDefinitionThroughRemovalAndReappearance(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestExternalMCPService(t)
	require.NoError(t, externalmcp.EnsureNativeCatalogSource(ctx, ti.conn))
	validator, err := mcpregistry.LoadValidator()
	require.NoError(t, err)
	registryService := mcpregistry.New(ti.conn, validator)
	const name = "io.example/retained-refresh"
	const installedURL = "https://example.com/installed"
	raw := json.RawMessage(`{"server":{"name":"io.example/retained-refresh","description":"Retained installed definition","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://example.com/default"},{"type":"streamable-http","url":"https://example.com/installed"}]}}`)
	source := &retainedCatalogRefreshSource{bundle: mcpregistry.CatalogBundle{Version: mcpregistry.CatalogBundleVersion, Entries: []json.RawMessage{raw}}}
	refresher := mcpregistry.NewCatalogRefresher(registryService, "local", "configured-production", source, nil)
	_, err = refresher.Import(ctx, "staff-owner")
	require.NoError(t, err)
	original, err := registryService.GetByName(ctx, name)
	require.NoError(t, err)
	reader := externalmcp.NewNativeRegistryReader(registryService)
	registry := externalmcp.Registry{ID: externalmcp.NativeCatalogRegistryID}
	// Installed-definition consumers retain their selected remote URLs instead of
	// performing the publication-gated dashboard/new-install lookup.
	installed, err := reader.GetServerDetails(ctx, registry, name, []string{installedURL})
	require.NoError(t, err)
	require.Equal(t, installedURL, installed.RemoteURL)
	payload := &gen.GetServerDetailsPayload{RegistryID: externalmcp.NativeCatalogRegistryID.String(), ServerSpecifier: name}
	_, err = ti.service.GetServerDetails(ctx, payload)
	require.NoError(t, err)

	source.bundle.Entries = nil
	plan, err := refresher.Preview(ctx, "staff-owner")
	require.NoError(t, err)
	require.Len(t, plan.Changes, 1)
	require.Equal(t, "unpublish", plan.Changes[0].Action)
	_, err = refresher.Apply(ctx, "staff-owner", plan.ID)
	require.NoError(t, err)
	hidden, err := registryService.GetByName(ctx, name)
	require.NoError(t, err)
	require.Equal(t, original.ID, hidden.ID)
	require.False(t, hidden.Published)
	discovery, err := reader.ListServers(ctx, registry, externalmcp.ListServersParams{})
	require.NoError(t, err)
	require.Empty(t, discovery.Servers)
	_, err = ti.service.GetServerDetails(ctx, payload)
	require.Error(t, err, "unpublished upstream removal cannot authorize a new installation")
	retained, err := reader.GetServerDetails(ctx, registry, name, []string{installedURL})
	require.NoError(t, err)
	require.Equal(t, installed, retained, "installed remote selection and definition survive removal")

	source.bundle.Entries = []json.RawMessage{raw}
	plan, err = refresher.Preview(ctx, "staff-owner")
	require.NoError(t, err)
	require.Equal(t, "republish", plan.Changes[0].Action)
	_, err = refresher.Apply(ctx, "staff-owner", plan.ID)
	require.NoError(t, err)
	restored, err := registryService.GetByName(ctx, name)
	require.NoError(t, err)
	require.Equal(t, original.ID, restored.ID)
	require.True(t, restored.Published)
	retained, err = reader.GetServerDetails(ctx, registry, name, []string{installedURL})
	require.NoError(t, err)
	require.Equal(t, installed, retained)
	discovery, err = reader.ListServers(ctx, registry, externalmcp.ListServersParams{})
	require.NoError(t, err)
	require.Len(t, discovery.Servers, 1)
	_, err = ti.service.GetServerDetails(ctx, payload)
	require.NoError(t, err, "safe reappearance makes new installation available again")
}
