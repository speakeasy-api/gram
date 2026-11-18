package admin

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

func TestSeedSupportMatrixRefreshesCatalogOwnedMethodColumns(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	require.NoError(t, SeedSupportMatrix(ctx, db))

	// A database seeded before LiteLLM became a vendor still files its method
	// under the old catch-all owner.
	_, err := db.Exec(ctx, "UPDATE support_matrix_integration_methods SET vendor = 'Others' WHERE slug = 'litellm'") //nolint:glint // notestingrawsql: recreates a row an older catalog seeded; only the seed writes a method's vendor, so no SQLc query can.
	require.NoError(t, err)
	stale, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)

	require.NoError(t, SeedSupportMatrix(ctx, db))
	snapshot, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	method := slices.IndexFunc(snapshot.Methods, func(m *gen.SupportMethod) bool { return m.ID == "litellm" })
	require.GreaterOrEqual(t, method, 0)
	require.Equal(t, "LiteLLM", snapshot.Methods[method].Vendor, "the catalog owns a method's vendor")
	require.NotEqual(t, stale.Revision, snapshot.Revision)
	for _, m := range snapshot.Methods {
		require.NotEqual(t, "Others", m.Vendor, m.ID)
	}
	gateway := slices.IndexFunc(snapshot.Products, func(p *gen.SupportPlatform) bool { return p.ID == "litellm-gateway" })
	require.GreaterOrEqual(t, gateway, 0)
	require.Equal(t, "LiteLLM", snapshot.Products[gateway].Vendor)

	// Reseeding an up-to-date database leaves the revision, which staff drafts
	// are checked against, unchanged.
	require.NoError(t, SeedSupportMatrix(ctx, db))
	same, err := svc.GetSupportMatrix(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, snapshot.Revision, same.Revision)
}
