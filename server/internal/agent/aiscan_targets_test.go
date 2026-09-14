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
	"github.com/speakeasy-api/gram/server/internal/conv"
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
			// Carried through, like the dashboard's own toggle does: a write
			// under a built-in's id must present that built-in unchanged, and
			// dropping the matchers here would read as an edit.
			GatewayClient: &gen.AiScanTargetGatewayClient{
				CimdVendorKeys:  target.GatewayClient.CIMDVendorKeys,
				OauthClientIds:  target.GatewayClient.OAuthClientIDs,
				ClientInfoNames: target.GatewayClient.ClientInfoNames,
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
	created, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAiScanTargetCreate)
	require.NoError(t, err)

	result, err := ti.service.UpsertAiScanTarget(ctx, chatgptDesktopPayload())
	require.NoError(t, err)
	require.EqualValues(t, aitargets.DefaultsVersion, result.ListVersion, "an edit changes the served targets and the ETag, not the version")
	require.Equal(t, "organization", result.Target.Origin)
	require.False(t, result.Target.Customized)
	require.True(t, result.Target.Enabled)
	require.NotNil(t, result.Target.CreatedAt)

	after, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	require.NotEqual(t, before.Etag, after.Etag, "a list change must move the poll etag")
	require.NotEqual(t, before.Configuration.Etag, after.Configuration.Etag)
	envelope := aiScanEnvelope(t, after)
	require.EqualValues(t, aitargets.DefaultsVersion, envelope["list_version"])
	require.Contains(t, servedTargetIDs(t, envelope), "chatgpt-desktop")

	listed, err := ti.service.ListAiScanTargets(ctx, &gen.ListAiScanTargetsPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Targets, len(aitargets.Defaults())+1)
	require.EqualValues(t, aitargets.DefaultsVersion, listed.ListVersion)

	createdAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAiScanTargetCreate)
	require.NoError(t, err)
	require.Equal(t, created+1, createdAfter)
	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionAiScanTargetCreate)
	require.NoError(t, err)
	require.Equal(t, ti.orgID+"/chatgpt-desktop", entry.SubjectID)
}

func TestUpsertAiScanTargetDisablesADefaultAndDeleteRestoresIt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))
	updated, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAiScanTargetUpdate)
	require.NoError(t, err)
	deleted, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAiScanTargetDelete)
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
	require.EqualValues(t, aitargets.DefaultsVersion, envelope["list_version"])

	listed, err := ti.service.ListAiScanTargets(ctx, &gen.ListAiScanTargetsPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Targets, len(aitargets.Defaults()), "a customized default stays in the list")
	updatedAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAiScanTargetUpdate)
	require.NoError(t, err)
	require.Equal(t, updated+1, updatedAfter, "customizing a default is recorded as an update of that default")

	restored, err := ti.service.DeleteAiScanTarget(ctx, &gen.DeleteAiScanTargetPayload{SessionToken: nil, ID: "aider"})
	require.NoError(t, err)
	require.EqualValues(t, aitargets.DefaultsVersion, restored.ListVersion)
	poll, err = ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	require.Contains(t, servedTargetIDs(t, aiScanEnvelope(t, poll)), "aider", "dropping the customization serves the default again")
	deletedAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAiScanTargetDelete)
	require.NoError(t, err)
	require.Equal(t, deleted+1, deletedAfter)
}

// firstDefaultWithGatewayMatchers picks a built-in that names a gateway caller.
// Which one that is moves as the registry grows — a vendor key is claimed only
// when the vendor publishes exactly one product — so the test asks rather than
// naming a product.
func firstDefaultWithGatewayMatchers() (aitargets.Target, bool) {
	for _, target := range aitargets.Defaults() {
		if !target.GatewayClient.IsZero() {
			return target, true
		}
	}
	return aitargets.ZeroTarget(), false
}

// gateway_client is optional on the wire, and a client that omits it is
// toggling the target, not unlinking it from the caller it blocks. For a
// built-in that carries matchers, treating the omission as a clear would make
// the built-in look edited and reject the toggle outright.
func TestUpsertAiScanTargetKeepsGatewayMatchersWhenTheFieldIsOmitted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	builtin, found := firstDefaultWithGatewayMatchers()
	require.True(t, found, "the case only bites for a built-in that has matchers")

	payload := defaultPayload(t, builtin.ID, false)
	payload.GatewayClient = nil

	result, err := ti.service.UpsertAiScanTarget(ctx, payload)
	require.NoError(t, err, "omitting gateway_client is a toggle, not an edit")
	require.False(t, result.Target.Enabled)
	require.ElementsMatch(t, builtin.GatewayClient.CIMDVendorKeys, result.Target.GatewayClient.CimdVendorKeys)
	require.ElementsMatch(t, builtin.GatewayClient.OAuthClientIDs, result.Target.GatewayClient.OauthClientIds)
	require.ElementsMatch(t, builtin.GatewayClient.ClientInfoNames, result.Target.GatewayClient.ClientInfoNames)
}

// A custom target keeps its matchers the same way, so an older client editing
// a display name cannot silently drop what a block rests on.
func TestUpsertAiScanTargetKeepsACustomTargetsMatchersWhenTheFieldIsOmitted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	payload := chatgptDesktopPayload()
	payload.GatewayClient = &gen.AiScanTargetGatewayClient{
		CimdVendorKeys:  nil,
		OauthClientIds:  nil,
		ClientInfoNames: []string{"acme-desktop"},
	}
	_, err := ti.service.UpsertAiScanTarget(ctx, payload)
	require.NoError(t, err)

	renamed := chatgptDesktopPayload()
	renamed.DisplayName = "Acme Desktop"
	renamed.GatewayClient = nil
	result, err := ti.service.UpsertAiScanTarget(ctx, renamed)
	require.NoError(t, err)
	require.Equal(t, []string{"acme-desktop"}, result.Target.GatewayClient.ClientInfoNames)

	cleared := chatgptDesktopPayload()
	cleared.GatewayClient = &gen.AiScanTargetGatewayClient{CimdVendorKeys: nil, OauthClientIds: nil, ClientInfoNames: nil}
	result, err = ti.service.UpsertAiScanTarget(ctx, cleared)
	require.NoError(t, err)
	require.Empty(t, result.Target.GatewayClient.ClientInfoNames, "sending the field empty still clears it")
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

func TestUpsertAiScanTargetKeepsAConfigDirAsItWasWritten(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	payload := chatgptDesktopPayload()
	payload.Signatures.ConfigDirs = []string{"~/Library/Application Support/com.openai.chat/", ".claude"}
	result, err := ti.service.UpsertAiScanTarget(ctx, payload)
	require.NoError(t, err)
	require.Equal(t,
		[]string{"~/Library/Application Support/com.openai.chat/", ".claude"},
		result.Target.Signatures.ConfigDirs,
	)
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
		_, err := queries.UpsertAIScanTarget(ctx, aitargets.UpsertParams(ti.orgID, filler))
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

// TestUpsertAiScanTargetRefusesToEditABuiltIn: built-ins are system-supplied
// and read-only. Enforced on the API and not only in the dashboard, because a
// hand-rolled call must not be able to silently redefine what every agent in
// the organization probes for.
func TestUpsertAiScanTargetRefusesToEditABuiltIn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)

	edited := defaultPayload(t, "cursor", true)
	edited.Signatures.Binaries = []string{"cursor", "cursor-nightly"}

	_, err := ti.service.UpsertAiScanTarget(ctx, edited)
	requireAiScanErrorCode(t, err, oops.CodeBadRequest, "a built-in's signatures cannot be rewritten")

	// Dropping a built-in's gateway matchers is an edit too: it would quietly
	// make a blocked tool unenforceable. Which built-in carries matchers moves
	// as the registry grows, so ask for one rather than naming a product: a
	// hardcoded id that stopped carrying matchers would leave this asserting
	// nothing.
	withMatchers, found := firstDefaultWithGatewayMatchers()
	require.True(t, found, "the case only bites for a built-in that has matchers")
	unlinked := defaultPayload(t, withMatchers.ID, true)
	unlinked.GatewayClient = &gen.AiScanTargetGatewayClient{
		CimdVendorKeys:  []string{},
		OauthClientIds:  []string{},
		ClientInfoNames: []string{},
	}
	_, err = ti.service.UpsertAiScanTarget(ctx, unlinked)
	requireAiScanErrorCode(t, err, oops.CodeBadRequest, "a built-in's gateway matchers cannot be dropped")

	// The one change an organization may make still works.
	_, err = ti.service.UpsertAiScanTarget(ctx, defaultPayload(t, "cursor", false))
	require.NoError(t, err, "switching a built-in off is not an edit")
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

// TestListBlockedAITargetIDsExcludesDisabledTargets guards the gateway's
// fast-path query directly.
//
// The check reads "does this organization block anything at all?" first, and
// only loads the catalog when the answer is yes. A disabled target is skipped
// by MatchGatewayCaller, so a block on one can never fire — but while its row
// still came back here the fast path stayed non-empty, forcing the catalog
// load. That load is answered with 503 on failure rather than by allowing, so
// a stale block on a switched-off tool turned a catalog outage into a refusal
// for the whole organization.
//
// Asserted on the query rather than through the handler on purpose: the
// handler allows either way, because MatchGatewayCaller skips the disabled
// target. The difference only shows under a catalog read failure, which there
// is no seam to inject today.
func TestListBlockedAITargetIDsExcludesDisabledTargets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)
	organizationID := ti.orgID

	queries := repo.New(ti.conn)

	_, err := queries.SetAIScanTargetStatus(ctx, repo.SetAIScanTargetStatusParams{
		OrganizationID: organizationID,
		ID:             "claude-code",
		Status:         "blocked",
		Rationale:      conv.ToPGTextEmpty("not approved"),
	})
	require.NoError(t, err)

	blocked, err := queries.ListBlockedAITargetIDs(ctx, organizationID)
	require.NoError(t, err)
	require.Contains(t, blocked, "claude-code", "an enabled blocked target must be on the fast path")

	_, err = queries.UpsertAIScanTarget(ctx, aitargets.BuiltInUpsertParams(organizationID, "claude-code", false))
	require.NoError(t, err)

	blocked, err = queries.ListBlockedAITargetIDs(ctx, organizationID)
	require.NoError(t, err)
	require.NotContains(t, blocked, "claude-code", "a block on a disabled target cannot fire, so it must not force the catalog load")
}
