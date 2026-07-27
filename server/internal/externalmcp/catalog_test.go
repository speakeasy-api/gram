package externalmcp_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
)

func TestExternalMCP_BuiltInGoogleCatalogEntries(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)
	result, err := ti.service.ListCatalog(ctx, &gen.ListCatalogPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		RegistryID:       nil,
		Search:           nil,
		Cursor:           nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 2)
	require.Equal(t, "com.google.workspace/drive", result.Servers[0].RegistrySpecifier)
	require.Equal(t, "com.google.workspace/docs", result.Servers[1].RegistrySpecifier)
	require.NotNil(t, result.Servers[1].RegistryID)

	details, err := ti.service.GetServerDetails(ctx, &gen.GetServerDetailsPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		RegistryID:       *result.Servers[1].RegistryID,
		ServerSpecifier:  result.Servers[1].RegistrySpecifier,
	})
	require.NoError(t, err)
	require.Len(t, details.Tools, 2)
	require.Equal(t, "read_doc", *details.Tools[0].Name)
	require.Equal(t, "update_doc", *details.Tools[1].Name)
}

func TestExternalMCP_BuiltInGoogleCatalogSearch(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestExternalMCPService(t)
	search := "Drive"
	result, err := ti.service.ListCatalog(ctx, &gen.ListCatalogPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		RegistryID:       nil,
		Search:           &search,
		Cursor:           nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Servers, 1)
	require.Equal(t, "com.google.workspace/drive", result.Servers[0].RegistrySpecifier)
}
