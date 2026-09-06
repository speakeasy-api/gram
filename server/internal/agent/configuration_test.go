package agent_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGetConfigurationReturnsUnconfiguredEnvelope(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	result, err := ti.service.GetConfiguration(ctx, &gen.GetConfigurationPayload{})
	require.NoError(t, err)
	require.False(t, result.IsConfigured)
	require.Equal(t, 1, result.SchemaVersion)
	require.Empty(t, result.Config)
	require.NotEmpty(t, result.Etag)
	require.Nil(t, result.UpdatedAt)
}

func TestGetConfigurationRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	// Viewing fleet configuration is org-admin only: an org reader (a plain
	// member) must be denied, not just a grant-less caller.
	for _, grants := range [][]authz.Grant{
		{},
		{authz.NewGrant(authz.ScopeOrgRead, ti.orgID)},
	} {
		scopedCtx := authztest.WithExactGrants(t, ctx, grants...)

		_, err := ti.service.GetConfiguration(scopedCtx, &gen.GetConfigurationPayload{})
		var shareableErr *oops.ShareableError
		require.ErrorAs(t, err, &shareableErr)
		require.Equal(t, oops.CodeForbidden, shareableErr.Code)
	}
}

func TestUpdateConfigurationPersistsAndDeliversOnPluginPoll(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))
	ctx = withPlatformAdmin(t, ctx)

	beforePoll, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	require.Nil(t, beforePoll.Configuration)

	beforeAuditCount, err := audittest.AuditLogCountByAction(
		ctx,
		ti.conn,
		audit.ActionOrganizationDeviceAgentConfigurationUpdated,
	)
	require.NoError(t, err)
	config := map[string]any{
		"platforms": map[string]any{
			"claude_code": "managed",
			"codex":       "user",
			"cursor":      false,
		},
		"update_channel":        "stable",
		"auto_update":           "notify",
		"sync_interval_seconds": 300,
		"blocked_versions":      []string{"1.2.3"},
		"future_setting":        map[string]any{"enabled": true},
	}

	updated, err := ti.service.UpdateConfiguration(ctx, &gen.UpdateConfigurationPayload{Config: config})
	require.NoError(t, err)
	require.True(t, updated.IsConfigured)
	require.Equal(t, 1, updated.SchemaVersion)
	require.Equal(t, "stable", updated.Config["update_channel"])
	require.Contains(t, updated.Config, "future_setting", "unknown keys must survive for forward compatibility")
	require.NotNil(t, updated.UpdatedAt)

	afterAuditCount, err := audittest.AuditLogCountByAction(
		ctx,
		ti.conn,
		audit.ActionOrganizationDeviceAgentConfigurationUpdated,
	)
	require.NoError(t, err)
	require.Equal(t, beforeAuditCount+1, afterAuditCount)

	dashboardView, err := ti.service.GetConfiguration(ctx, &gen.GetConfigurationPayload{})
	require.NoError(t, err)
	require.Equal(t, updated, dashboardView)

	afterPoll, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	require.NotNil(t, afterPoll.Configuration)
	require.Equal(t, updated, afterPoll.Configuration)
	require.NotEqual(t, beforePoll.Etag, afterPoll.Etag, "remote configuration changes must invalidate the policy etag")
}

func TestUpdateConfigurationRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, ti.orgID))

	_, err := ti.service.UpdateConfiguration(ctx, &gen.UpdateConfigurationPayload{Config: map[string]any{}})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)
}

func TestUpdateConfigurationRejectsDeviceIdentityAndSecrets(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	_, err := ti.service.UpdateConfiguration(ctx, &gen.UpdateConfigurationPayload{
		Config: map[string]any{"org_token": "must-not-leave-the-device"},
	})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeInvalid, shareableErr.Code)
}

func TestUpdateConfigurationRejectsInvalidPlatformLayer(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	_, err := ti.service.UpdateConfiguration(ctx, &gen.UpdateConfigurationPayload{
		Config: map[string]any{
			"platforms": map[string]any{"cursor": true},
		},
	})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeInvalid, shareableErr.Code)
}

func TestUpdateConfigurationAcceptsAIScanInterval(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	updated, err := ti.service.UpdateConfiguration(ctx, &gen.UpdateConfigurationPayload{
		Config: map[string]any{"ai_scan_interval_seconds": 21600},
	})
	require.NoError(t, err)
	require.True(t, updated.IsConfigured)

	// The setting flows to agents through the existing configuration blob on
	// the plugin poll, like every other admin-saved key.
	poll, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	require.NotNil(t, poll.Configuration)
	require.EqualValues(t, 21600, poll.Configuration.Config["ai_scan_interval_seconds"])
}

func TestUpdateConfigurationRejectsInvalidAIScanInterval(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	for _, invalid := range []any{"six hours", 0, 59, 24*60*60 + 1, 1.5} {
		_, err := ti.service.UpdateConfiguration(ctx, &gen.UpdateConfigurationPayload{
			Config: map[string]any{"ai_scan_interval_seconds": invalid},
		})
		var shareableErr *oops.ShareableError
		require.ErrorAs(t, err, &shareableErr, "value %v must be rejected", invalid)
		require.Equal(t, oops.CodeInvalid, shareableErr.Code)
	}
}

func TestUpdateConfigurationPreservesStoredUnknownKeys(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))
	ctx = withPlatformAdmin(t, ctx)

	_, err := ti.service.UpdateConfiguration(ctx, &gen.UpdateConfigurationPayload{Config: map[string]any{
		"pinned_target":  "1.2.3",
		"future_setting": map[string]any{"enabled": true},
	}})
	require.NoError(t, err)

	updated, err := ti.service.UpdateConfiguration(ctx, &gen.UpdateConfigurationPayload{Config: map[string]any{
		"update_channel": "beta",
	}})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"enabled": true}, updated.Config["future_setting"], "stored unknown keys must survive updates that omit them")
	require.Equal(t, "beta", updated.Config["update_channel"])
	require.NotContains(t, updated.Config, "pinned_target", "omitting a known key must remove it")
}

func TestUpdateConfigurationRejectsPlatformAdminOnlyKeysForOrgAdmins(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	for _, key := range []string{"update_channel", "blocked_versions"} {
		_, err := ti.service.UpdateConfiguration(ctx, &gen.UpdateConfigurationPayload{
			Config: map[string]any{key: "stable"},
		})
		var shareableErr *oops.ShareableError
		require.ErrorAs(t, err, &shareableErr, "org admins must not set %s", key)
		require.Equal(t, oops.CodeForbidden, shareableErr.Code)
	}
}

func TestUpdateConfigurationPreservesPlatformAdminOnlyKeysForOrgAdmins(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	_, err := ti.service.UpdateConfiguration(withPlatformAdmin(t, ctx), &gen.UpdateConfigurationPayload{Config: map[string]any{
		"update_channel":   "beta",
		"blocked_versions": []string{"1.2.3"},
	}})
	require.NoError(t, err)

	updated, err := ti.service.UpdateConfiguration(ctx, &gen.UpdateConfigurationPayload{Config: map[string]any{
		"auto_update": "notify",
	}})
	require.NoError(t, err)
	require.Equal(t, "beta", updated.Config["update_channel"], "org admin updates must not remove platform-admin-only keys by omission")
	require.Equal(t, []any{"1.2.3"}, updated.Config["blocked_versions"])
	require.Equal(t, "notify", updated.Config["auto_update"])
}

func TestUpdateConfigurationRejectsNewerStoredSchemaVersion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	_, err := agentrepo.New(ti.conn).UpsertDeviceAgentConfiguration(ctx, agentrepo.UpsertDeviceAgentConfigurationParams{
		OrganizationID: ti.orgID,
		SchemaVersion:  2,
		Config:         []byte(`{"future_setting":true}`),
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateConfiguration(ctx, &gen.UpdateConfigurationPayload{
		Config: map[string]any{"auto_update": "notify"},
	})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeConflict, shareableErr.Code)
}
