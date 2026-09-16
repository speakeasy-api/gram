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
	}
}

// defaultPayload builds an upsert payload from a compiled-in default, the
// way the dashboard writes one back.
func defaultPayload(t *testing.T, id string) *gen.UpsertAiScanTargetPayload {
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
			// Carried through, the way the dashboard does: a write under a
			// built-in's id must present that built-in unchanged, and dropping
			// the matchers here would read as an edit.
			GatewayClient: &gen.AiScanTargetGatewayClient{
				CimdVendorKeys:  target.GatewayClient.CIMDVendorKeys,
				OauthClientIds:  target.GatewayClient.OAuthClientIDs,
				ClientInfoNames: target.GatewayClient.ClientInfoNames,
			},
			VersionPlistKey: nil,
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

// TestUpsertAiScanTargetRecordsARowForADefaultAndDeleteDropsIt: a write under
// a built-in's id stores the organization's row for it and nothing else — the
// definition stays compiled in, and the target is probed for either way
// because it is in the inventory either way. Deleting the row drops the
// customization and leaves the built-in exactly as it shipped.
func TestUpsertAiScanTargetRecordsARowForADefaultAndDeleteDropsIt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))
	updated, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAiScanTargetUpdate)
	require.NoError(t, err)
	deleted, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAiScanTargetDelete)
	require.NoError(t, err)

	// The row a built-in carries is written by the access-decision endpoint,
	// which is the only thing an organization can record about one. Upsert
	// refuses a built-in outright, so the row is seeded here the way that
	// endpoint seeds it.
	_, err = repo.New(ti.conn).SetAIScanTargetStatus(ctx, repo.SetAIScanTargetStatusParams{
		OrganizationID: ti.orgID,
		ID:             "aider",
		Status:         "blocked",
		Rationale:      conv.ToPGTextEmpty("not approved"),
	})
	require.NoError(t, err)

	poll, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	envelope := aiScanEnvelope(t, poll)
	require.Contains(t, servedTargetIDs(t, envelope), "aider", "a built-in with a row is still in the inventory, so it is still probed for")
	require.EqualValues(t, aitargets.DefaultsVersion, envelope["list_version"])

	listed, err := ti.service.ListAiScanTargets(ctx, &gen.ListAiScanTargetsPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Targets, len(aitargets.Defaults()), "a default carrying a decision stays in the list")
	for _, entry := range listed.Targets {
		if entry.ID == "aider" {
			require.True(t, entry.Customized, "a row makes the built-in customized")
		}
	}
	updatedAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAiScanTargetUpdate)
	require.NoError(t, err)
	require.Equal(t, updated, updatedAfter, "seeding the row directly is not an upsert, so no update is logged")

	restored, err := ti.service.DeleteAiScanTarget(ctx, &gen.DeleteAiScanTargetPayload{SessionToken: nil, ID: "aider"})
	require.NoError(t, err)
	require.EqualValues(t, aitargets.DefaultsVersion, restored.ListVersion)

	listed, err = ti.service.ListAiScanTargets(ctx, &gen.ListAiScanTargetsPayload{SessionToken: nil})
	require.NoError(t, err)
	require.Len(t, listed.Targets, len(aitargets.Defaults()))
	for _, entry := range listed.Targets {
		if entry.ID == "aider" {
			require.False(t, entry.Customized, "dropping the row leaves the built-in as it shipped")
		}
	}
	poll, err = ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	require.Contains(t, servedTargetIDs(t, aiScanEnvelope(t, poll)), "aider")
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
	var lastFiller string
	for i := len(aitargets.Defaults()); i < aitargets.MaxTargets; i++ {
		filler := aitargets.Target{
			ID:          "filler-" + string(rune('a'+i%26)) + "-" + string(rune('a'+i/26)),
			DisplayName: "Filler",
			Category:    aitargets.CategoryHarness,
			Signatures:  aitargets.Signatures{BundleIDs: []string{}, Binaries: []string{"filler"}, ConfigDirs: []string{}, ProcessNames: []string{}},
			VersionHint: nil,
		}
		_, err := queries.UpsertAIScanTarget(ctx, aitargets.UpsertParams(ti.orgID, filler))
		require.NoError(t, err)
		lastFiller = filler.ID
	}
	require.NotEmpty(t, lastFiller)

	_, err := ti.service.UpsertAiScanTarget(ctx, chatgptDesktopPayload())
	requireAiScanErrorCode(t, err, oops.CodeBadRequest)

	// Leaving the inventory is the only thing that frees a slot: everything
	// in it is served, so deleting one of the organization's own targets is
	// what makes room for another.
	_, err = ti.service.DeleteAiScanTarget(ctx, &gen.DeleteAiScanTargetPayload{SessionToken: nil, ID: lastFiller})
	require.NoError(t, err)
	_, err = ti.service.UpsertAiScanTarget(ctx, chatgptDesktopPayload())
	require.NoError(t, err, "deleting a target frees a slot")
}

// TestUpsertAiScanTargetRefusesToEditABuiltIn: built-ins are system-supplied
// and read-only. Enforced on the API and not only in the dashboard, because a
// hand-rolled call must not be able to silently redefine what every agent in
// the organization probes for.
func TestUpsertAiScanTargetRefusesToEditABuiltIn(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestAgentService(t)

	edited := defaultPayload(t, "cursor")
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
	unlinked := defaultPayload(t, withMatchers.ID)
	unlinked.GatewayClient = &gen.AiScanTargetGatewayClient{
		CimdVendorKeys:  []string{},
		OauthClientIds:  []string{},
		ClientInfoNames: []string{},
	}
	_, err = ti.service.UpsertAiScanTarget(ctx, unlinked)
	requireAiScanErrorCode(t, err, oops.CodeBadRequest, "a built-in's gateway matchers cannot be dropped")

	// Presenting the built-in unchanged is refused too, and that is the point
	// of the rule rather than an edge of it. There is nothing left to write
	// about a built-in: its definition is compiled in and every organization
	// is served it. Accepting the write would record a row carrying nothing,
	// mark the built-in customized and log an update that changed nothing.
	_, err = ti.service.UpsertAiScanTarget(ctx, defaultPayload(t, "cursor"))
	requireAiScanErrorCode(t, err, oops.CodeBadRequest, "there is nothing to write about a built-in")
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

// TestUpsertAiScanTargetClearsTheDecisionWhenEnforceabilityIsLost: the upsert
// replaces the definition and never touches status, so an edit that clears a
// target's last gateway matcher would leave a block recorded that nothing can
// enforce, and adding a matcher back later would revive it with nobody having
// decided again. Losing enforceability clears the decision instead, and that
// clearing is logged as the decision change it is.
func TestUpsertAiScanTargetClearsTheDecisionWhenEnforceabilityIsLost(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))

	linked := chatgptDesktopPayload()
	linked.GatewayClient = &gen.AiScanTargetGatewayClient{
		CimdVendorKeys:  []string{},
		OauthClientIds:  []string{"https://client.example/desktop/client.json"},
		ClientInfoNames: []string{},
	}
	_, err := ti.service.UpsertAiScanTarget(ctx, linked)
	require.NoError(t, err)

	// Block it the way the decision endpoint does.
	_, err = repo.New(ti.conn).SetAIScanTargetStatus(ctx, repo.SetAIScanTargetStatusParams{
		OrganizationID: ti.orgID,
		ID:             linked.ID,
		Status:         "blocked",
		Rationale:      conv.ToPGTextEmpty("not approved"),
	})
	require.NoError(t, err)
	decisions, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAIToolDecisionSet)
	require.NoError(t, err)

	statusOf := func() string {
		rows, err := repo.New(ti.conn).ListAIScanTargets(ctx, ti.orgID)
		require.NoError(t, err)
		for _, row := range rows {
			if row.ID == linked.ID {
				return row.Status
			}
		}
		require.FailNow(t, "target row missing")
		return ""
	}
	require.Equal(t, "blocked", statusOf())

	// The edit that removes the last matcher.
	unlinked := chatgptDesktopPayload()
	unlinked.GatewayClient = &gen.AiScanTargetGatewayClient{CimdVendorKeys: []string{}, OauthClientIds: []string{}, ClientInfoNames: []string{}}
	_, err = ti.service.UpsertAiScanTarget(ctx, unlinked)
	require.NoError(t, err)
	require.Equal(t, "unreviewed", statusOf(), "a block nothing can enforce must not outlive the edit that made it unenforceable")

	decisionsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionAIToolDecisionSet)
	require.NoError(t, err)
	require.Equal(t, decisions+1, decisionsAfter, "clearing the decision is a decision change and is logged as one")

	// Restoring the matcher does not restore the block: the organization
	// decides again once the target can be recognized at the gateway.
	_, err = ti.service.UpsertAiScanTarget(ctx, linked)
	require.NoError(t, err)
	require.Equal(t, "unreviewed", statusOf(), "re-adding a matcher must not silently reactivate the old block")
}
