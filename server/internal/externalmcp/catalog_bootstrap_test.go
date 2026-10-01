package externalmcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	registryrepo "github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
)

func TestNativeNamespaceBootstrap(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestExternalMCPService(t)
	require.NoError(t, externalmcp.EnsureNativeCatalogSource(ctx, ti.conn))
	require.NoError(t, externalmcp.EnsureNativeCatalogSource(ctx, ti.conn))
	var count int
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT count(*) FROM mcp_registries WHERE id=$1 AND url=$2`, externalmcp.NativeCatalogRegistryID, externalmcp.NativeCatalogRegistryURL).Scan(&count))
	require.Equal(t, 1, count)
	_, err := ti.conn.Exec(ctx, `UPDATE mcp_registries SET enabled=false WHERE id=$1`, externalmcp.NativeCatalogRegistryID)
	require.NoError(t, err)
	require.NoError(t, externalmcp.EnsureNativeCatalogSource(ctx, ti.conn))
	var enabled bool
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT enabled FROM mcp_registries WHERE id=$1`, externalmcp.NativeCatalogRegistryID).Scan(&enabled))
	require.False(t, enabled)
	_, err = ti.conn.Exec(ctx, `UPDATE mcp_registries SET url='https://wrong.example' WHERE id=$1`, externalmcp.NativeCatalogRegistryID)
	require.NoError(t, err)
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
	id := uuid.New()
	_, err := ti.conn.Exec(ctx, `INSERT INTO mcp_registries (id,name,url) VALUES ($1,'Existing',$2)`, id, externalmcp.NativeCatalogRegistryURL)
	require.NoError(t, err)
	require.Error(t, externalmcp.EnsureNativeCatalogSource(ctx, ti.conn))
	var actual uuid.UUID
	require.NoError(t, ti.conn.QueryRow(ctx, `SELECT id FROM mcp_registries WHERE url=$1`, externalmcp.NativeCatalogRegistryURL).Scan(&actual))
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
	_, err = ti.conn.Exec(ctx, `UPDATE mcp_registry_entries SET published=false WHERE id=$1`, id)
	require.NoError(t, err)
	_, err = ti.service.GetServerDetails(ctx, payload)
	require.Error(t, err, "new preinstall details must recheck publication")
}
