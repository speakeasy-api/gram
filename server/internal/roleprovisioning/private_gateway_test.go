package roleprovisioning_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestPrivateGatewayOriginRemovalOnRemapAllowsNarrowing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	v := f.configure(0, true)
	original := f.reconcile(f.role).PluginID
	gatewayID := uuid.New()
	// Legacy invalid Everyone state must not prevent removing the origin role.
	f.exec(`INSERT INTO plugin_assignments (plugin_id,organization_id,principal_urn) VALUES ($1,$2,$3)`, original, f.org, urn.PrincipalWildcard)
	f.exec(`INSERT INTO meta_mcp_servers (id,organization_id,project_id,name,network_access_mode) VALUES ($1,$2,$3,'Private gateway','private_only')`, gatewayID, f.org, f.project)
	f.exec(`INSERT INTO plugin_servers (plugin_id,project_id,meta_mcp_server_id,display_name) VALUES ($1,$2,$3,'Private gateway')`, original, f.project, gatewayID)
	destination := f.addProject(f.org, "remapped")
	f.configure(v, true, roleprovisioning.Selection{RoleURN: f.role, Enabled: true, ProjectID: &destination})
	moved := f.reconcile(f.role)
	require.Empty(t, moved.Pending)
	require.NotEqual(t, uuid.Nil, moved.PluginID)
	require.NotEqual(t, original, moved.PluginID)
	require.Equal(t, []string{urn.PrincipalWildcard}, f.audiences(original))
	require.Equal(t, []string{f.role}, f.audiences(moved.PluginID))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM role_plugin_associations WHERE plugin_id=$1 AND NOT is_current`, original))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM role_plugin_associations WHERE plugin_id=$1 AND is_current`, moved.PluginID))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM plugin_servers WHERE plugin_id=$1 AND meta_mcp_server_id=$2 AND NOT deleted`, original, gatewayID))
	f.assertEmpty(moved.PluginID)
}

func TestPrivateGatewayAudienceRepairRemainsPending(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.configure(0, true)
	pluginID := f.reconcile(f.role).PluginID
	gatewayID := uuid.New()
	// Reproduce legacy state that current admission would refuse to create.
	// Adding the missing role must still check the complete desired audience.
	f.exec(`DELETE FROM plugin_assignments WHERE plugin_id=$1`, pluginID)
	f.exec(`INSERT INTO plugin_assignments (plugin_id,organization_id,principal_urn) VALUES ($1,$2,$3)`, pluginID, f.org, urn.PrincipalWildcard)
	f.exec(`INSERT INTO meta_mcp_servers (id,organization_id,project_id,name,network_access_mode) VALUES ($1,$2,$3,'Private gateway','private_only')`, gatewayID, f.org, f.project)
	f.exec(`INSERT INTO plugin_servers (plugin_id,project_id,meta_mcp_server_id,display_name) VALUES ($1,$2,$3,'Private gateway')`, pluginID, f.project, gatewayID)
	audits, err := audittest.AuditLogCount(t.Context(), f.db)
	require.NoError(t, err)
	outbox := f.count(`SELECT count(*) FROM publish_outbox`)

	result, err := f.service.Reconcile(t.Context(), f.org, f.role, f.actor)
	require.NoError(t, err)
	require.Equal(t, "private_gateway_audience", result.Pending)
	require.Equal(t, []string{urn.PrincipalWildcard}, f.audiences(pluginID))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM role_plugin_associations WHERE plugin_id=$1 AND is_current`, pluginID))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM plugins WHERE id=$1 AND name='Engineers' AND NOT deleted`, pluginID))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM plugin_servers WHERE plugin_id=$1 AND meta_mcp_server_id=$2 AND NOT deleted`, pluginID, gatewayID))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM role_provisioning_settings WHERE organization_id=$1 AND role_urn=$2 AND project_id=$3 AND enabled AND last_error_code='private_gateway_audience' AND last_attempt_at IS NOT NULL`, f.org, f.role, f.project))
	afterAudits, err := audittest.AuditLogCount(t.Context(), f.db)
	require.NoError(t, err)
	require.Equal(t, audits, afterAudits)
	require.Equal(t, outbox, f.count(`SELECT count(*) FROM publish_outbox`))
}
