package roleprovisioning_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/stretchr/testify/require"
)

func (f *fixture) attachDirectRemote(pluginID, projectID uuid.UUID) {
	f.t.Helper()
	remote, server := uuid.New(), uuid.New()
	f.exec(`INSERT INTO remote_mcp_servers (id,project_id,transport_type,url) VALUES ($1,$2,'streamable-http','https://mcp.example.test/api')`, remote, projectID)
	f.exec(`INSERT INTO mcp_servers (id,project_id,remote_mcp_server_id,visibility) VALUES ($1,$2,$3,'public')`, server, projectID, remote)
	f.exec(`INSERT INTO platform_mcp_catalog_registrations (organization_id,project_id,source_kind,catalog_provider,catalog_reference,mcp_server_id) VALUES ($1,$2,'remote','direct-remote-url-v1','https://mcp.example.test/api',$3)`, f.org, projectID, server)
	f.exec(`INSERT INTO plugin_servers (plugin_id,mcp_server_id,display_name) VALUES ($1,$2,'Admin content')`, pluginID, server)
}

func TestAudienceRepairAndRemapFailClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, code string
		enforce    bool
	}{
		{name: "unavailable", code: "admission_unavailable"},
		{name: "approval required", code: "audience_approval_required", enforce: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			v := f.configure(0, true)
			a := f.reconcile(f.role).PluginID
			f.attachDirectRemote(a, f.project)
			// Unrelated stale principals must survive failed repair without revalidation.
			stale := "role:organization:" + uuid.NewString()
			f.exec(`DELETE FROM plugin_assignments WHERE plugin_id=$1`, a)
			f.exec(`INSERT INTO plugin_assignments (plugin_id,organization_id,principal_urn) VALUES ($1,$2,$3)`, a, f.org, stale)
			if tc.enforce {
				flags := new(feature.InMemory)
				flags.SetFlag(feature.FlagPlatformMCPShadowAudienceEnforcement, f.org, true)
				flags.SetFlag(feature.FlagPlatformMCPDirectRemoteDistributionDisabled, f.org, false)
				f.exec(`INSERT INTO risk_policies (organization_id,project_id,name,sources,action,version) VALUES ($1,$2,'Approval required',ARRAY['shadow_mcp'],'block',1)`, f.org, f.project)
				flags.SetFlagPayload(feature.FlagPlatformMCPShadowAudienceEnforcement, f.org, []byte(`{"mode":"enforce"}`))
				f.service = roleprovisioning.New(f.db, audit.NewLogger(), admission.NewGuard(flags, nil), plugins.PublicationRequests{})
			}
			pending := f.reconcile(f.role)
			require.Equal(t, tc.code, pending.Pending)
			require.Equal(t, []string{stale}, f.audiences(a))
			require.Equal(t, 1, f.count(`SELECT count(*) FROM role_plugin_associations WHERE plugin_id=$1 AND is_current`, a))
			// A newly-created destination is empty and remains provisionable even when
			// policy is unavailable for populated plugins.
			b := f.addProject(f.org, "destination")
			v = f.configure(v, true, roleprovisioning.Selection{RoleURN: f.role, Enabled: true, ProjectID: &b})
			moved := f.reconcile(f.role)
			require.Empty(t, moved.Pending)
			require.NotEqual(t, uuid.Nil, moved.PluginID)
			require.Equal(t, []string{f.role}, f.audiences(moved.PluginID))
			f.configure(v, true, roleprovisioning.Selection{RoleURN: f.role, Enabled: true, ProjectID: &f.project})
			refused := f.reconcile(f.role)
			require.Equal(t, tc.code, refused.Pending)
			require.Equal(t, []string{f.role}, f.audiences(moved.PluginID))
			require.Equal(t, []string{stale}, f.audiences(a))
			require.Equal(t, 1, f.count(`SELECT count(*) FROM role_plugin_associations WHERE plugin_id=$1 AND is_current`, moved.PluginID))
			require.Equal(t, 1, f.count(`SELECT count(*) FROM role_provisioning_settings WHERE organization_id=$1 AND role_urn=$2 AND project_id=$3 AND last_error_code=$4`, f.org, f.role, f.project, tc.code))
			require.Equal(t, 1, f.count(`SELECT count(*) FROM plugin_servers WHERE plugin_id=$1 AND NOT deleted`, a))
		})
	}
}

func TestConfigurationOutboxFailureRollsBackIntent(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	// This trigger exists only in this test's disposable cloned database.
	f.exec(`CREATE FUNCTION reject_provisioning_hint() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected outbox failure'; END $$`)
	f.exec(`CREATE TRIGGER reject_hint BEFORE INSERT ON publish_outbox FOR EACH ROW EXECUTE FUNCTION reject_provisioning_hint()`)
	_, err := f.service.Configure(t.Context(), roleprovisioning.ConfigureInput{OrganizationID: f.org, Enabled: true})
	require.ErrorContains(t, err, "injected outbox failure")
	require.Zero(t, f.count(`SELECT count(*) FROM organization_role_provisioning_settings`))
	require.Zero(t, f.count(`SELECT count(*) FROM role_provisioning_settings`))
	require.Zero(t, f.count(`SELECT count(*) FROM plugins`))
}

func TestPublicationHandoffIsAtomicAndDistinct(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.service = roleprovisioning.New(f.db, audit.NewLogger(), admission.NewGuard(nil, nil), plugins.PublicationRequests{Enabled: true})
	f.configure(0, true)
	unconfigured := f.reconcile(f.role)
	require.Equal(t, plugins.ProjectPublicationNotConfigured, unconfigured.Publication)
	require.NotEqual(t, uuid.Nil, unconfigured.PluginID)
	f.exec(`INSERT INTO plugin_github_connections (project_id,installation_id,repo_owner,repo_name) VALUES ($1,1,'example','fixture')`, f.project)
	second := f.addRole(f.org, "New role")
	f.exec(`CREATE FUNCTION reject_publication() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.topic='gram.plugins.v1.PublicationRequested' THEN RAISE EXCEPTION 'injected publication failure'; END IF; RETURN NEW; END $$`)
	f.exec(`CREATE TRIGGER reject_publication BEFORE INSERT ON publish_outbox FOR EACH ROW EXECUTE FUNCTION reject_publication()`)
	_, err := f.service.Reconcile(t.Context(), f.org, second, f.actor)
	require.ErrorContains(t, err, "injected publication failure")
	require.Equal(t, 1, f.count(`SELECT count(*) FROM plugins`))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM role_plugin_associations`))
	require.Equal(t, 1, f.count(`SELECT count(*) FROM role_provisioning_settings`))
	f.exec(`DROP TRIGGER reject_publication ON publish_outbox`)
	created := f.reconcile(second)
	require.Equal(t, plugins.ProjectPublicationEnqueued, created.Publication)
	require.Equal(t, 1, f.count(`SELECT count(*) FROM publish_outbox WHERE topic='gram.plugins.v1.PublicationRequested'`))
	require.Equal(t, 2, f.count(`SELECT count(*) FROM plugins`))
}
