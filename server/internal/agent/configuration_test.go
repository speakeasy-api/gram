package agent_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestGetConfigurationReturnsUnconfiguredEnvelope(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, ti.orgID))

	result, err := ti.service.GetConfiguration(ctx, &gen.GetConfigurationPayload{})
	require.NoError(t, err)
	require.False(t, result.IsConfigured)
	require.Equal(t, 1, result.SchemaVersion)
	require.Empty(t, result.Config)
	require.NotEmpty(t, result.Etag)
	require.Nil(t, result.UpdatedAt)
}

func TestGetConfigurationRequiresOrganizationRead(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	_, err := ti.service.GetConfiguration(ctx, &gen.GetConfigurationPayload{})
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr)
	require.Equal(t, oops.CodeForbidden, shareableErr.Code)
}

func TestUpdateConfigurationPersistsAndDeliversOnPluginPoll(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	beforePoll, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: "developer@example.com"})
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

	afterPoll, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: "developer@example.com"})
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
