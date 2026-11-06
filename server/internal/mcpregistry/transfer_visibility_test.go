package mcpregistry

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogTransferPreservesDeletedVisibility(t *testing.T) {
	t.Parallel()
	ctx, source, _ := newTestService(t)
	const name = "example.test/tombstone"
	raw := json.RawMessage(`{"server":{"name":"example.test/tombstone","version":"1","description":"Synthetic deleted definition"},"_meta":{"io.modelcontextprotocol.registry/official":{"status":"deleted"}}}`)
	_, err := source.Create(ctx, raw)
	require.NoError(t, err)
	bundle, err := source.ExportCatalog(ctx, nil)
	require.NoError(t, err)
	require.Len(t, bundle.Entries, 1)

	_, destination, _ := newTestService(t)
	_, err = NewCatalogImporter(destination, "local", nil).Import(ctx, bundle)
	require.NoError(t, err)
	for _, service := range []*Service{source, destination} {
		page, err := service.Discover(ctx, DiscoveryOptions{})
		require.NoError(t, err)
		require.Empty(t, page.Records, "transfer must not resurrect deleted definitions in catalog discovery")
		_, err = service.LookupDiscoveryVersion(ctx, name, "latest", false)
		require.ErrorIs(t, err, ErrNotFound)
		retained, err := service.LookupDiscoveryVersion(ctx, name, "latest", true)
		require.NoError(t, err)
		var record struct {
			Meta map[string]struct {
				Status string `json:"status"`
			} `json:"_meta"`
		}
		require.NoError(t, json.Unmarshal(retained.Data, &record))
		require.Equal(t, "deleted", record.Meta["io.modelcontextprotocol.registry/official"].Status)
	}
}
