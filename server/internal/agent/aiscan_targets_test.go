package agent_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func aiScanEnvelope(t *testing.T, poll *gen.GetPluginsResult) map[string]any {
	t.Helper()
	require.NotNil(t, poll.Configuration)
	envelope, ok := poll.Configuration.Config["ai_scan"].(map[string]any)
	require.True(t, ok, "plugin poll must carry the ai_scan envelope")
	return envelope
}

func servedTargetIDs(t *testing.T, envelope map[string]any) []string {
	t.Helper()
	targets, ok := envelope["targets"].([]any)
	require.True(t, ok)
	ids := make([]string, 0, len(targets))
	for _, target := range targets {
		entry, ok := target.(map[string]any)
		require.True(t, ok)
		id, ok := entry["id"].(string)
		require.True(t, ok)
		ids = append(ids, id)
	}
	return ids
}

func requireAiScanErrorCode(t *testing.T, err error, code oops.Code, msgAndArgs ...any) {
	t.Helper()
	var shareableErr *oops.ShareableError
	require.ErrorAs(t, err, &shareableErr, msgAndArgs...)
	require.Equal(t, code, shareableErr.Code, msgAndArgs...)
}

func chatgptDesktopPayload() *gen.UpsertAiScanTargetPayload {
	return &gen.UpsertAiScanTargetPayload{
		SessionToken: nil,
		ID:           "chatgpt-desktop",
		DisplayName:  "ChatGPT Desktop",
		Category:     "harness",
		Signatures: &gen.AiScanTargetSignatures{
			BundleIds:    []string{},
			Binaries:     []string{"chatgpt"},
			ConfigDirs:   []string{},
			ProcessNames: []string{},
		},
		VersionPlistKey: nil,
		Enabled:         true,
	}
}

// defaultPayload builds an upsert payload from a compiled-in default, the
// way the dashboard customizes one.
func defaultPayload(t *testing.T, id string, enabled bool) *gen.UpsertAiScanTargetPayload {
	t.Helper()
	for _, target := range aitargets.Defaults() {
		if target.ID != id {
			continue
		}
		return &gen.UpsertAiScanTargetPayload{
			SessionToken: nil,
			ID:           target.ID,
			DisplayName:  target.DisplayName,
			Category:     string(target.Category),
			Signatures: &gen.AiScanTargetSignatures{
				BundleIds:    target.Signatures.BundleIDs,
				Binaries:     target.Signatures.Binaries,
				ConfigDirs:   target.Signatures.ConfigDirs,
				ProcessNames: target.Signatures.ProcessNames,
			},
			VersionPlistKey: nil,
			Enabled:         enabled,
		}
	}
	t.Fatalf("no default target %q", id)
	return nil
}

func TestGetPluginsServesTheDefaultsToAnOrganizationWithNoTargets(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	poll, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)

	require.False(t, poll.Configuration.IsConfigured)
	envelope := aiScanEnvelope(t, poll)
	require.EqualValues(t, aitargets.SchemaVersion, envelope["schema_version"])
	require.EqualValues(t, aitargets.DefaultsVersion, envelope["list_version"])
	require.NotEmpty(t, envelope["etag"])
	ids := servedTargetIDs(t, envelope)
	require.Len(t, ids, len(aitargets.Defaults()))
	require.Equal(t, "aider", ids[0], "targets are served ordered by id")
}

func TestGetPluginsServesAIScanCatalogAlongsideAdminSettings(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	_, err := ti.service.UpdateConfiguration(ctx, &gen.UpdateConfigurationPayload{
		Config: map[string]any{"ai_scan_interval_seconds": 21600},
	})
	require.NoError(t, err)

	poll, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	require.True(t, poll.Configuration.IsConfigured)
	require.EqualValues(t, 21600, poll.Configuration.Config["ai_scan_interval_seconds"])
	aiScanEnvelope(t, poll)

	stored, err := ti.service.GetConfiguration(ctx, &gen.GetConfigurationPayload{})
	require.NoError(t, err)
	require.NotContains(t, stored.Config, "ai_scan")
}

func TestUpsertAiScanTargetAddsATargetAgentsReceive(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	before, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	created, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDeviceAgentAiScanTargetCreate)
	require.NoError(t, err)

	result, err := ti.service.UpsertAiScanTarget(ctx, chatgptDesktopPayload())
	require.NoError(t, err)
	require.EqualValues(t, aitargets.DefaultsVersion+1, result.ListVersion)
	require.Equal(t, "organization", result.Target.Origin)
	require.False(t, result.Target.Customized)
	require.True(t, result.Target.Enabled)
	require.NotNil(t, result.Target.CreatedAt)

	after, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	require.NotEqual(t, before.Etag, after.Etag, "a list change must move the poll etag")
	require.NotEqual(t, before.Configuration.Etag, after.Configuration.Etag)
	envelope := aiScanEnvelope(t, after)
	require.EqualValues(t, aitargets.DefaultsVersion+1, envelope["list_version"])
	require.Contains(t, servedTargetIDs(t, envelope), "chatgpt-desktop")

	listed, err := ti.service.ListAiScanTargets(ctx, &gen.ListAiScanTargetsPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Targets, len(aitargets.Defaults())+1)
	require.EqualValues(t, aitargets.DefaultsVersion+1, listed.ListVersion)

	createdAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDeviceAgentAiScanTargetCreate)
	require.NoError(t, err)
	require.Equal(t, created+1, createdAfter)
	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDeviceAgentAiScanTargetCreate)
	require.NoError(t, err)
	require.Equal(t, ti.orgID+"/chatgpt-desktop", entry.SubjectID)
}

func TestUpsertAiScanTargetDisablesADefaultAndDeleteRestoresIt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))
	updated, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDeviceAgentAiScanTargetUpdate)
	require.NoError(t, err)
	deleted, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDeviceAgentAiScanTargetDelete)
	require.NoError(t, err)

	result, err := ti.service.UpsertAiScanTarget(ctx, defaultPayload(t, "aider", false))
	require.NoError(t, err)
	require.Equal(t, "default", result.Target.Origin)
	require.True(t, result.Target.Customized)
	require.False(t, result.Target.Enabled)

	poll, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	envelope := aiScanEnvelope(t, poll)
	require.NotContains(t, servedTargetIDs(t, envelope), "aider", "a disabled default is not served")
	require.EqualValues(t, aitargets.DefaultsVersion+1, envelope["list_version"])

	listed, err := ti.service.ListAiScanTargets(ctx, &gen.ListAiScanTargetsPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Targets, len(aitargets.Defaults()), "a customized default stays in the list")
	updatedAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDeviceAgentAiScanTargetUpdate)
	require.NoError(t, err)
	require.Equal(t, updated+1, updatedAfter, "customizing a default is recorded as an update of that default")

	restored, err := ti.service.DeleteAiScanTarget(ctx, &gen.DeleteAiScanTargetPayload{SessionToken: nil, ID: "aider"})
	require.NoError(t, err)
	require.EqualValues(t, aitargets.DefaultsVersion+2, restored.ListVersion)
	poll, err = ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	require.Contains(t, servedTargetIDs(t, aiScanEnvelope(t, poll)), "aider", "dropping the customization serves the default again")
	deletedAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDeviceAgentAiScanTargetDelete)
	require.NoError(t, err)
	require.Equal(t, deleted+1, deletedAfter)
}

func TestDeleteAiScanTargetRefusesIdsTheOrganizationDoesNotOwn(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	_, err := ti.service.DeleteAiScanTarget(ctx, &gen.DeleteAiScanTargetPayload{SessionToken: nil, ID: "codex"})
	requireAiScanErrorCode(t, err, oops.CodeNotFound)
	_, err = ti.service.DeleteAiScanTarget(ctx, &gen.DeleteAiScanTargetPayload{SessionToken: nil, ID: "never-added"})
	requireAiScanErrorCode(t, err, oops.CodeNotFound)
}

func TestUpsertAiScanTargetRejectsWhatAgentsWouldRefuse(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	payload := chatgptDesktopPayload()
	payload.Signatures.Binaries = []string{"../../usr/bin/chatgpt"}
	_, err := ti.service.UpsertAiScanTarget(ctx, payload)
	requireAiScanErrorCode(t, err, oops.CodeBadRequest)

	payload = chatgptDesktopPayload()
	payload.Signatures.Binaries = []string{}
	_, err = ti.service.UpsertAiScanTarget(ctx, payload)
	requireAiScanErrorCode(t, err, oops.CodeBadRequest)
}

func TestUpsertAiScanTargetRefusesToGrowTheServedSetPastWhatAgentsAccept(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	queries := repo.New(ti.conn)
	for i := len(aitargets.Defaults()); i < aitargets.MaxTargets; i++ {
		filler := aitargets.Target{
			ID:          "filler-" + string(rune('a'+i%26)) + "-" + string(rune('a'+i/26)),
			DisplayName: "Filler",
			Category:    aitargets.CategoryHarness,
			Signatures:  aitargets.Signatures{BundleIDs: []string{}, Binaries: []string{"filler"}, ConfigDirs: []string{}, ProcessNames: []string{}},
			VersionHint: nil,
			Enabled:     true,
		}
		_, err := queries.UpsertDeviceAgentAIScanTarget(ctx, aitargets.UpsertParams(ti.orgID, filler))
		require.NoError(t, err)
	}

	_, err := ti.service.UpsertAiScanTarget(ctx, chatgptDesktopPayload())
	requireAiScanErrorCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.UpsertAiScanTarget(ctx, defaultPayload(t, "aider", false))
	require.NoError(t, err, "disabling a default frees a slot")
	_, err = ti.service.UpsertAiScanTarget(ctx, chatgptDesktopPayload())
	require.NoError(t, err)
	_, err = ti.service.DeleteAiScanTarget(ctx, &gen.DeleteAiScanTargetPayload{SessionToken: nil, ID: "aider"})
	requireAiScanErrorCode(t, err, oops.CodeBadRequest, "restoring the default would overfill the served set")
}

func TestAiScanTargetEndpointsRequireOrganizationAdmin(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, ti.orgID))

	_, err := ti.service.ListAiScanTargets(ctx, &gen.ListAiScanTargetsPayload{SessionToken: nil})
	requireAiScanErrorCode(t, err, oops.CodeForbidden)
	_, err = ti.service.UpsertAiScanTarget(ctx, chatgptDesktopPayload())
	requireAiScanErrorCode(t, err, oops.CodeForbidden)
	_, err = ti.service.DeleteAiScanTarget(ctx, &gen.DeleteAiScanTargetPayload{SessionToken: nil, ID: "aider"})
	requireAiScanErrorCode(t, err, oops.CodeForbidden)
}

func TestUpdateConfigurationRejectsAIScanKey(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))
	ctx = withPlatformAdmin(t, ctx)

	_, err := ti.service.UpdateConfiguration(ctx, &gen.UpdateConfigurationPayload{
		Config: map[string]any{"ai_scan": map[string]any{"targets": []any{}}},
	})
	requireAiScanErrorCode(t, err, oops.CodeInvalid)
}

func TestStoredAIScanKeyIsNeverServedAndDoesNotShapeTheEtag(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	queries := repo.New(ti.conn)
	_, err := queries.UpsertDeviceAgentConfiguration(ctx, repo.UpsertDeviceAgentConfigurationParams{
		OrganizationID: ti.orgID,
		SchemaVersion:  1,
		Config:         []byte(`{"ai_scan":{"legacy":true},"ai_scan_interval_seconds":21600}`),
	})
	require.NoError(t, err)

	stored, err := ti.service.GetConfiguration(ctx, &gen.GetConfigurationPayload{})
	require.NoError(t, err)
	require.NotContains(t, stored.Config, "ai_scan")
	require.EqualValues(t, 21600, stored.Config["ai_scan_interval_seconds"])

	poll, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	envelope := aiScanEnvelope(t, poll)
	require.NotContains(t, envelope, "legacy")
	require.Contains(t, envelope, "targets")

	_, err = queries.UpsertDeviceAgentConfiguration(ctx, repo.UpsertDeviceAgentConfigurationParams{
		OrganizationID: ti.orgID,
		SchemaVersion:  1,
		Config:         []byte(`{"ai_scan_interval_seconds":21600}`),
	})
	require.NoError(t, err)
	clean, err := ti.service.GetConfiguration(ctx, &gen.GetConfigurationPayload{})
	require.NoError(t, err)
	require.Equal(t, clean.Etag, stored.Etag, "the etag must hash the served document, not the stored one")
}
