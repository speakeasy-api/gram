package externalmcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	externalrepo "github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	registryrepo "github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
)

func TestNativeNamespaceBootstrap(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestExternalMCPService(t)
	require.NoError(t, externalmcp.EnsureNativeCatalogSource(ctx, ti.conn))
	require.NoError(t, externalmcp.EnsureNativeCatalogSource(ctx, ti.conn))
	repository := externalrepo.New(ti.conn)
	rows, err := repository.ListMCPRegistries(ctx)
	require.NoError(t, err)
	var count int
	for _, row := range rows {
		if row.ID == externalmcp.NativeCatalogRegistryID && row.Url == externalmcp.NativeCatalogRegistryURL {
			count++
		}
	}
	require.Equal(t, 1, count)
	require.NoError(t, repository.SetMCPRegistryEnabledFixture(ctx, externalrepo.SetMCPRegistryEnabledFixtureParams{ID: externalmcp.NativeCatalogRegistryID, Enabled: pgtype.Bool{Bool: false, Valid: true}}))
	require.NoError(t, externalmcp.EnsureNativeCatalogSource(ctx, ti.conn))
	row, err := repository.GetMCPRegistryByID(ctx, externalmcp.NativeCatalogRegistryID)
	require.NoError(t, err)
	require.True(t, row.Enabled.Valid)
	require.False(t, row.Enabled.Bool)
	require.NoError(t, repository.SetMCPRegistryURLFixture(ctx, externalrepo.SetMCPRegistryURLFixtureParams{ID: externalmcp.NativeCatalogRegistryID, Url: "https://wrong.example"}))
	require.Error(t, externalmcp.EnsureNativeCatalogSource(ctx, ti.conn))
}

func TestNativeRetainedEvidenceNotDiscovery(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestExternalMCPService(t)
	for _, row := range []struct{ name, status string }{{"retained", "active"}, {"deleted", "deleted"}} {
		raw := []byte(`{"server":{"name":"io.example/` + row.name + `","description":"Evidence","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://example.com/mcp"}]},"_meta":{"io.modelcontextprotocol.registry/official":{"status":"` + row.status + `"}}}`)
		require.NoError(t, registryrepo.New(ti.conn).InsertRegistryEntryFixture(ctx, registryrepo.InsertRegistryEntryFixtureParams{ID: uuid.New(), Data: raw, Published: false}))
	}
	validator, err := mcpregistry.LoadValidator()
	require.NoError(t, err)
	reader := externalmcp.NewNativeRegistryReader(mcpregistry.New(ti.conn, validator))
	registry := externalmcp.Registry{ID: externalmcp.NativeCatalogRegistryID}
	list, err := reader.ListServers(ctx, registry, externalmcp.ListServersParams{})
	require.NoError(t, err)
	require.Empty(t, list.Servers)
	evidence, err := reader.ListEvidenceServers(ctx, registry)
	require.NoError(t, err)
	require.Len(t, evidence.Servers, 1)
	require.Equal(t, "io.example/retained", evidence.Servers[0].RegistrySpecifier)
}

func TestNativeNamespaceBootstrapConflictingURL(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestExternalMCPService(t)
	repository := externalrepo.New(ti.conn)
	id, err := repository.CreateMCPRegistry(ctx, externalrepo.CreateMCPRegistryParams{Name: "Existing", Url: externalmcp.NativeCatalogRegistryURL})
	require.NoError(t, err)
	require.Error(t, externalmcp.EnsureNativeCatalogSource(ctx, ti.conn))
	rows, err := repository.ListMCPRegistries(ctx)
	require.NoError(t, err)
	var actual uuid.UUID
	for _, row := range rows {
		if row.Url == externalmcp.NativeCatalogRegistryURL {
			actual = row.ID
		}
	}
	require.Equal(t, id, actual)
}

func TestNativeDashboardDetailsRejectUnpublishedSelection(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestExternalMCPService(t)
	require.NoError(t, externalmcp.EnsureNativeCatalogSource(ctx, ti.conn))
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.feature.SetFlag(feature.FlagGramMCPCatalog, auth.ActiveOrganizationID, true)
	id := uuid.New()
	raw := []byte(`{"server":{"name":"io.example/selection","description":"Selection","version":"1.0.0","remotes":[{"type":"streamable-http","url":"https://example.com/mcp"}]},"_meta":{"io.modelcontextprotocol.registry/official":{"status":"active"}}}`)
	require.NoError(t, registryrepo.New(ti.conn).InsertRegistryEntryFixture(ctx, registryrepo.InsertRegistryEntryFixtureParams{ID: id, Data: raw, Published: true}))
	payload := &gen.GetServerDetailsPayload{RegistryID: externalmcp.NativeCatalogRegistryID.String(), ServerSpecifier: "io.example/selection"}
	_, err := ti.service.GetServerDetails(ctx, payload)
	require.NoError(t, err)
	err = registryrepo.New(ti.conn).SetRegistryEntryPublishedFixture(ctx, registryrepo.SetRegistryEntryPublishedFixtureParams{ID: id, Published: false})
	require.NoError(t, err)
	_, err = ti.service.GetServerDetails(ctx, payload)
	require.Error(t, err, "new preinstall details must recheck publication")
}
