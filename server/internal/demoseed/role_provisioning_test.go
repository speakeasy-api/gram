package demoseed

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The shared fixture is deliberately inert and tenant-scoped. The DB-backed
// safety suite additionally executes it twice and checks other tenants' rows.
func TestRoleProvisioningFixtureDefaultOff(t *testing.T) {
	t.Parallel()
	for _, spec := range []Spec{DefaultSpec(), LocalSpec()} {
		script := spec.Rewrite(postgresSQL)
		require.Contains(t, script, "DELETE FROM organization_role_provisioning_settings WHERE organization_id = demo_org;")
		require.Contains(t, script, "INSERT INTO organization_role_provisioning_settings (organization_id, enabled, project_id, version)\n  VALUES (demo_org, FALSE, NULL, 0);")
		require.Contains(t, script, "WHERE organization_id = demo_org AND enabled IS FALSE\n    AND project_id IS NULL AND version = 0;")
		require.Contains(t, script, "expected 1 default-off role provisioning setting")
	}
}
