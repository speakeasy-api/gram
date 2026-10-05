package deployments_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/assets/assetstest"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
)

func TestCatalogAdmissionPreservesOnlyPersistedIdentity(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestDeploymentService(t, assetstest.NewTestBlobStore(t))
	id := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	saved := []repo.ListDeploymentExternalMCPsRow{{RegistryID: id, Slug: "test", RegistryServerSpecifier: "example/server"}}

	// The catalog knows no registry with this id, so it cannot admit new work,
	// but it must not gate an identity the deployment already accepted.
	for _, tc := range []struct {
		name       string
		registryID uuid.NullUUID
		slug       string
		server     string
		accepted   bool
	}{
		{"same identity", id, "test", "example/server", true},
		{"same slug different registry", uuid.NullUUID{UUID: uuid.New(), Valid: true}, "test", "example/server", false},
		{"same slug different server", id, "test", "example/other", false},
		{"new attachment slug", id, "other", "example/server", false},
		{"direct attachment", uuid.NullUUID{}, "test", "example/server", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ti.service.AdmitExternalMCPSelection(ctx, "org-test", "test-org", tc.registryID, tc.slug, tc.server, saved, nil)
			if tc.accepted {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, externalmcp.ErrCatalogSourceNotFound)
			}
		})
	}
	require.ErrorIs(t, ti.service.AdmitExternalMCPSelection(ctx, "org-test", "test-org", id, "test", "example/server", saved, []string{"test"}), externalmcp.ErrCatalogSourceNotFound)
}
