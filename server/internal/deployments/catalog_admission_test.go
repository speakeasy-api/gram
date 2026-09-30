package deployments

import (
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/deployments/repo"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCatalogAdmissionPreservesOnlyPersistedIdentity(t *testing.T) {
	id := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	saved := []repo.ListDeploymentExternalMCPsRow{{RegistryID: id, Slug: "test", RegistryServerSpecifier: "example/server"}}
	base := upsertExternalMCP{registryID: id, slug: "test", registryServerSpecifier: "example/server"}
	// A missing catalog cannot admit new work, but must not gate accepted identity.
	svc := &Service{}
	for _, tc := range []struct {
		name     string
		change   func(*upsertExternalMCP)
		accepted bool
	}{
		{"same identity after rollout change", func(*upsertExternalMCP) {}, true},
		{"display name and remotes are mutable", func(v *upsertExternalMCP) {
			v.name = "Renamed"
			v.selectedRemotes = []string{"https://example.test/mcp"}
		}, true},
		{"same slug different registry", func(v *upsertExternalMCP) { v.registryID.UUID = uuid.New() }, false},
		{"same slug different server", func(v *upsertExternalMCP) { v.registryServerSpecifier = "example/other" }, false},
		{"new attachment slug", func(v *upsertExternalMCP) { v.slug = "other" }, false},
		{"direct attachment", func(v *upsertExternalMCP) { v.registryID = uuid.NullUUID{} }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := base
			tc.change(&candidate)
			err := svc.admitExternalMCPSelections(t.Context(), "org-test", "test-org", []upsertExternalMCP{candidate}, saved, nil)
			if tc.accepted {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, externalmcp.ErrCatalogSourceNotFound)
			}
		})
	}
	require.ErrorIs(t, svc.admitExternalMCPSelections(t.Context(), "org-test", "test-org", []upsertExternalMCP{base}, saved, []string{"test"}), externalmcp.ErrCatalogSourceNotFound)
	require.NoError(t, svc.admitExternalMCPSelections(t.Context(), "org-test", "test-org", nil, saved, nil))
}
