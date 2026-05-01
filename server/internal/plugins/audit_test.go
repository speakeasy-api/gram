package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
)

func TestPluginsService_CreatePlugin_RecordsAuditEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginCreate)
	require.NoError(t, err)

	created, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Audit Create"})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionPluginCreate)
	require.NoError(t, err)
	require.Equal(t, "plugin", rec.SubjectType)
	require.Equal(t, created.Name, rec.SubjectDisplay)
	require.Equal(t, created.Slug, rec.SubjectSlug)
}

func TestPluginsService_UpdatePlugin_RecordsAuditEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	created, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Audit Update"})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)

	_, err = ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{
		ID:          created.ID,
		Name:        "Audit Update Renamed",
		Slug:        "audit-update-renamed",
		Description: nil,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)
	require.Equal(t, "plugin", rec.SubjectType)
	require.Equal(t, "audit-update-renamed", rec.SubjectSlug)
	require.NotEmpty(t, rec.BeforeSnapshot)
	require.NotEmpty(t, rec.AfterSnapshot)

	beforeSnap, err := audittest.DecodeAuditData(rec.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, "Audit Update", beforeSnap["name"])
	require.Equal(t, created.Slug, beforeSnap["slug"])

	afterSnap, err := audittest.DecodeAuditData(rec.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "Audit Update Renamed", afterSnap["name"])
	require.Equal(t, "audit-update-renamed", afterSnap["slug"])
}

func TestPluginsService_DeletePlugin_RecordsAuditEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	created, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Audit Delete"})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginDelete)
	require.NoError(t, err)

	require.NoError(t, ti.service.DeletePlugin(ctx, &gen.DeletePluginPayload{ID: created.ID}))

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginDelete)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionPluginDelete)
	require.NoError(t, err)
	require.Equal(t, "plugin", rec.SubjectType)
	require.Equal(t, created.Slug, rec.SubjectSlug)
}

func TestPluginsService_AddPluginServer_RecordsAuditEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Audit ServerAdd"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "audit-add")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerAdd)
	require.NoError(t, err)

	server, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   toolset.ID.String(),
		DisplayName: "Server A",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerAdd)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionPluginServerAdd)
	require.NoError(t, err)
	require.Equal(t, "plugin", rec.SubjectType)
	require.Equal(t, plugin.Slug, rec.SubjectSlug)

	meta, err := audittest.DecodeAuditData(rec.Metadata)
	require.NoError(t, err)
	require.Equal(t, server.ID, meta["server_id"])
	require.Equal(t, "Server A", meta["server_display_name"])
	require.Equal(t, "required", meta["server_policy"])
}

func TestPluginsService_UpdatePluginServer_RecordsAuditEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Audit ServerUpdate"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "audit-update-srv")

	server, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   toolset.ID.String(),
		DisplayName: "Original Server",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerUpdate)
	require.NoError(t, err)

	_, err = ti.service.UpdatePluginServer(ctx, &gen.UpdatePluginServerPayload{
		ID:          server.ID,
		PluginID:    plugin.ID,
		DisplayName: "Renamed Server",
		Policy:      "optional",
		SortOrder:   5,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionPluginServerUpdate)
	require.NoError(t, err)
	require.Equal(t, "plugin", rec.SubjectType)

	meta, err := audittest.DecodeAuditData(rec.Metadata)
	require.NoError(t, err)
	require.Equal(t, "Renamed Server", meta["server_display_name"])
	require.Equal(t, "optional", meta["server_policy"])
}

func TestPluginsService_RemovePluginServer_RecordsAuditEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Audit ServerRemove"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "audit-remove-srv")

	server, err := ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   toolset.ID.String(),
		DisplayName: "Doomed Server",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerRemove)
	require.NoError(t, err)

	require.NoError(t, ti.service.RemovePluginServer(ctx, &gen.RemovePluginServerPayload{
		ID:       server.ID,
		PluginID: plugin.ID,
	}))

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerRemove)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionPluginServerRemove)
	require.NoError(t, err)
	require.Equal(t, "plugin", rec.SubjectType)

	meta, err := audittest.DecodeAuditData(rec.Metadata)
	require.NoError(t, err)
	require.Equal(t, server.ID, meta["server_id"])
}

func TestPluginsService_SetPluginAssignments_RecordsAuditEvent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestPluginsService(t)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Audit Assignments"})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginAssignmentsSet)
	require.NoError(t, err)

	urns := []string{"role:engineering", "role:gtm"}
	_, err = ti.service.SetPluginAssignments(ctx, &gen.SetPluginAssignmentsPayload{
		PluginID:      plugin.ID,
		PrincipalUrns: urns,
	})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginAssignmentsSet)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionPluginAssignmentsSet)
	require.NoError(t, err)
	require.Equal(t, "plugin", rec.SubjectType)
	require.Equal(t, plugin.Slug, rec.SubjectSlug)

	meta, err := audittest.DecodeAuditData(rec.Metadata)
	require.NoError(t, err)
	got, ok := meta["principal_urns"].([]any)
	require.True(t, ok)
	require.Len(t, got, 2)
	require.Equal(t, "role:engineering", got[0])
	require.Equal(t, "role:gtm", got[1])
}

func TestPluginsService_PublishPlugins_RecordsAuditEvent(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Audit Publish"})
	require.NoError(t, err)

	toolset := createTestToolset(t, ctx, ti.conn, "audit-publish")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   toolset.ID.String(),
		DisplayName: "Publish Server",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginPublish)
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginPublish)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionPluginPublish)
	require.NoError(t, err)
	require.Equal(t, "project", rec.SubjectType)

	meta, err := audittest.DecodeAuditData(rec.Metadata)
	require.NoError(t, err)
	require.Equal(t, "test-org", meta["repo_owner"])
	slugs, ok := meta["plugin_slugs"].([]any)
	require.True(t, ok)
	require.Len(t, slugs, 1)
	require.Equal(t, plugin.Slug, slugs[0])
}

func TestPluginsService_PublishPlugins_RecordsPluginScopedKeyCreate(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Audit Key Create"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "audit-key-create-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   toolset.ID.String(),
		DisplayName: "S",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)

	// The most recent api_key:create event should be the per-server plugin
	// key (mcp keys are created before the hooks key in the publish loop —
	// LatestAuditLogByAction returns by created_at DESC, so the hooks key
	// is last). Decode metadata to verify plugin_id + toolset_id surfaced.
	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionKeyCreate)
	require.NoError(t, err)
	meta, err := audittest.DecodeAuditData(rec.Metadata)
	require.NoError(t, err)
	// The hooks key has neither plugin_id nor toolset_id, so look at the
	// per-server key by querying the audit table directly with the metadata
	// filter.
	var pluginScopedMeta []byte
	err = ti.conn.QueryRow(ctx, `
		SELECT metadata FROM audit_logs
		WHERE action = $1 AND metadata->>'plugin_id' IS NOT NULL
		ORDER BY created_at DESC LIMIT 1
	`, string(audit.ActionKeyCreate)).Scan(&pluginScopedMeta)
	require.NoError(t, err)

	pluginMeta, err := audittest.DecodeAuditData(pluginScopedMeta)
	require.NoError(t, err)
	require.Equal(t, plugin.ID, pluginMeta["plugin_id"], "plugin_id must surface in audit metadata")
	require.Equal(t, toolset.ID.String(), pluginMeta["toolset_id"], "toolset_id must surface in audit metadata")

	// Still verify the latest record looks valid (it's the hooks key).
	require.Equal(t, "api_key", rec.SubjectType)
	require.Contains(t, meta, "scopes")
}

func TestPluginsService_PublishPlugins_RepublishRecordsKeyRevoke(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	plugin, err := ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Audit Key Revoke"})
	require.NoError(t, err)
	toolset := createTestToolset(t, ctx, ti.conn, "audit-key-revoke-toolset")
	_, err = ti.service.AddPluginServer(ctx, &gen.AddPluginServerPayload{
		PluginID:    plugin.ID,
		ToolsetID:   toolset.ID.String(),
		DisplayName: "S",
		Policy:      "required",
		SortOrder:   0,
	})
	require.NoError(t, err)

	// First publish: no prior keys to revoke.
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	revokesBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)

	// Second publish: must revoke the prior generation's per-server key.
	_, err = ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
	revokesAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)
	require.Equal(t, revokesBefore+1, revokesAfter, "republish must emit one revoke audit per rotated key")

	rec, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)
	require.Equal(t, "api_key", rec.SubjectType)
	meta, err := audittest.DecodeAuditData(rec.Metadata)
	require.NoError(t, err)
	require.Equal(t, plugin.ID, meta["plugin_id"], "revoke audit must carry plugin_id of the rotated key")
	require.Equal(t, toolset.ID.String(), meta["toolset_id"], "revoke audit must carry toolset_id of the rotated key")
}
