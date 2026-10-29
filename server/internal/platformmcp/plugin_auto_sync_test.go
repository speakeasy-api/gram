package platformmcp

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	plugindelivery "github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// stubAutoSyncGuard stands in for the plugins service's turn-off check. It
// records which plugins were checked, before the write and inside it, and
// refuses the ones listed in unsynced the way the service does.
type stubAutoSyncGuard struct {
	mu                   sync.Mutex
	unsynced             map[uuid.UUID]bool
	checked              []uuid.UUID
	checkedInTransaction []uuid.UUID
}

func (g *stubAutoSyncGuard) RequireAutoSyncCanTurnOff(_ context.Context, _ string, _, pluginID uuid.UUID) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checked = append(g.checked, pluginID)
	return g.refusal(pluginID)
}

func (g *stubAutoSyncGuard) RequireAutoSyncCanTurnOffInTransaction(_ context.Context, _ pgx.Tx, _ string, _, pluginID uuid.UUID) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.checkedInTransaction = append(g.checkedInTransaction, pluginID)
	return g.refusal(pluginID)
}

func (g *stubAutoSyncGuard) refusal(pluginID uuid.UUID) error {
	if g.unsynced[pluginID] {
		return unsyncedChangesRefusal()
	}
	return nil
}

// unsyncedChangesRefusal is the plugins service's refusal for a plugin with
// changes that haven't been synced.
func unsyncedChangesRefusal() error {
	return oops.E(oops.CodeFailedPrecondition, plugindelivery.ErrPluginHasUnsyncedChanges, "This plugin has changes that haven't been synced yet.")
}

func (g *stubAutoSyncGuard) checks() []uuid.UUID {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.checked)
}

func (g *stubAutoSyncGuard) checksInTransaction() []uuid.UUID {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.checkedInTransaction)
}

// slugPublicationEvidence answers for exactly the slugs asked about, from a
// fixed freshness per slug.
type slugPublicationEvidence map[string]*bool

func (e slugPublicationEvidence) ResolvePublicationEvidence(_ context.Context, _ string, _ uuid.UUID, slugs []string) ([]plugindelivery.PublicationEvidence, error) {
	out := make([]plugindelivery.PublicationEvidence, 0, len(slugs))
	for _, slug := range slugs {
		out = append(out, plugindelivery.PublicationEvidence{PluginSlug: slug, NotConfigured: false, Fresh: e[slug], Packages: nil})
	}
	return out, nil
}

func seedPausedPlugin(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID, name, slug string) {
	t.Helper()
	seedPluginWithAutoSync(t, ctx, conn, organizationID, projectID, name, slug, pgtype.Bool{Bool: false, Valid: true})
}

// The admin inventory reports each plugin's auto sync, and get_plugin says
// when changes are waiting for a manual sync: auto sync off and the package not
// fresh, the dashboard's "Auto sync off" with "Needs syncing".
func TestPluginReadsReportAutoSyncAndChangesAwaitingAManualSync(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_plugin_auto_sync_reads")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Active", "active")
	seedPausedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Paused Stale", "paused-stale")
	seedPausedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Paused Current", "paused-current")
	service := testPluginTargets(conn).WithPublicationEvidence(slugPublicationEvidence{
		"active": new(false), "paused-stale": new(false), "paused-current": new(true),
	})
	ctx = withOrganizationGrant(ctx, authz.ScopeOrgAdmin, principal.OrganizationID)

	page, err := service.ListPlugins(ctx, principal, ListPluginsInput{ProjectID: project.ID.String()})
	require.NoError(t, err)
	listed := map[string]*bool{}
	for _, plugin := range page.Plugins {
		listed[plugin.Slug] = plugin.AutoSyncEnabled
	}
	require.Equal(t, map[string]*bool{"active": new(true), "paused-stale": new(false), "paused-current": new(false)}, listed)

	for slug, awaiting := range map[string]bool{"active": false, "paused-stale": true, "paused-current": false} {
		got, err := service.GetPlugin(ctx, principal, GetPluginInput{ProjectID: project.ID.String(), Plugin: slug})
		require.NoError(t, err, slug)
		require.Equal(t, listed[slug], got.Plugin.AutoSyncEnabled, slug)
		require.NotNil(t, got.PublicationEvidence, slug)
		require.Equal(t, awaiting, got.PublicationEvidence.AwaitingManualSync, slug)
	}
}

func TestCreatePluginCanStartWithAutoSyncOff(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedPluginMetadataFixture(t, "platform_mcp_create_plugin_auto_sync")

	defaulted, err := fixture.service.CreatePlugin(ctx, fixture.principal, CreatePluginInput{
		ProjectID: fixture.project.ID.String(), Name: "Syncs By Default", IdempotencyKey: "create-default", Confirmed: true,
	})
	require.NoError(t, err)
	require.Equal(t, new(true), defaulted.Plugin.AutoSyncEnabled, "auto sync is on unless asked otherwise")

	// Asking for the default by name is the same request as leaving it out.
	replayed, err := fixture.service.CreatePlugin(ctx, fixture.principal, CreatePluginInput{
		ProjectID: fixture.project.ID.String(), Name: "Syncs By Default", AutoSyncEnabled: new(true), IdempotencyKey: "create-default", Confirmed: true,
	})
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)

	input := CreatePluginInput{
		ProjectID: fixture.project.ID.String(), Name: "Held Back", AutoSyncEnabled: new(false), IdempotencyKey: "create-paused", Confirmed: true,
	}
	paused, err := fixture.service.CreatePlugin(ctx, fixture.principal, input)
	require.NoError(t, err)
	require.Equal(t, new(false), paused.Plugin.AutoSyncEnabled)
	got, err := fixture.service.GetPlugin(ctx, fixture.principal, GetPluginInput{ProjectID: fixture.project.ID.String(), Plugin: paused.Plugin.ID})
	require.NoError(t, err)
	require.Equal(t, new(false), got.Plugin.AutoSyncEnabled)

	// The same key with auto sync left on is a different request.
	input.AutoSyncEnabled = nil
	_, err = fixture.service.CreatePlugin(ctx, fixture.principal, input)
	var refusal *PluginMetadataMutationError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, pluginMetadataCodeIdempotencyKeyReused, refusal.Code)
}

// Turning auto sync off and back on changes nothing else about the plugin,
// is audited with the setting before and after, returns the committed state,
// and replays from its receipt.
func TestSetPluginAutoSyncTurnsItOffAndOn(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedPluginMetadataFixture(t, "platform_mcp_set_plugin_auto_sync")
	guard := &stubAutoSyncGuard{}
	signaler := &recordingPluginPublishSignaler{}
	fixture.service.WithAutoSyncGuard(guard).WithRepublish(plugindelivery.PublicationRequests{Enabled: false}, signaler, testOperationBudget())
	plugin, err := pluginsrepo.New(fixture.conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: fixture.principal.OrganizationID, ProjectID: fixture.project.ID,
		Name: "Support Tools", Slug: "support-tools", Description: pgtype.Text{String: "Kept as is", Valid: true},
	})
	require.NoError(t, err)
	before, err := fixture.service.GetPlugin(ctx, fixture.principal, GetPluginInput{ProjectID: fixture.project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	beforeAudit, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)

	off := SetPluginAutoSyncInput{ProjectID: fixture.project.ID.String(), Plugin: "Support Tools", AutoSyncEnabled: false, IdempotencyKey: "auto-sync-off", Confirmed: true}
	turnedOff, err := fixture.service.SetPluginAutoSync(ctx, fixture.principal, off)
	require.NoError(t, err)
	require.Equal(t, pluginMetadataAutoSyncTurnedOff, turnedOff.ResultCategory)
	require.Equal(t, new(false), turnedOff.Plugin.AutoSyncEnabled)
	require.Equal(t, []uuid.UUID{plugin.ID}, guard.checks(), "turning auto sync off runs the dashboard's check")
	require.Equal(t, []uuid.UUID{plugin.ID}, guard.checksInTransaction(), "and runs it again in the transaction that writes the setting")

	after, err := fixture.service.GetPlugin(ctx, fixture.principal, GetPluginInput{ProjectID: fixture.project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	expected := before.Plugin
	expected.AutoSyncEnabled = new(false)
	require.Equal(t, expected, after.Plugin, "only auto sync changes: name, slug, description, contents, assignments, and publication stay put")

	afterAudit, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAudit+1, afterAudit)
	record, err := audittest.LatestAuditLogByAction(ctx, fixture.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)
	beforeSnapshot, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	afterSnapshot, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, true, beforeSnapshot["auto_sync_enabled"])
	require.Equal(t, false, afterSnapshot["auto_sync_enabled"])
	require.Equal(t, "Support Tools", afterSnapshot["name"])

	// A retry replays the stored change without checking or writing again.
	retry, err := fixture.service.SetPluginAutoSync(ctx, fixture.principal, off)
	require.NoError(t, err)
	require.True(t, retry.Receipt.Replayed)
	require.Equal(t, turnedOff.Plugin, retry.Plugin)
	require.Len(t, guard.checks(), 1)
	require.Len(t, guard.checksInTransaction(), 1)
	replayAudit, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)
	require.Equal(t, afterAudit, replayAudit)

	// Turning it back on needs no check, as in the dashboard.
	turnedOn, err := fixture.service.SetPluginAutoSync(ctx, fixture.principal, SetPluginAutoSyncInput{
		ProjectID: fixture.project.ID.String(), Plugin: plugin.ID.String(), AutoSyncEnabled: true, IdempotencyKey: "auto-sync-on", Confirmed: true,
	})
	require.NoError(t, err)
	require.Equal(t, pluginMetadataAutoSyncTurnedOn, turnedOn.ResultCategory)
	require.Equal(t, new(true), turnedOn.Plugin.AutoSyncEnabled)
	require.Len(t, guard.checks(), 1)
	require.Len(t, guard.checksInTransaction(), 1)
	// Like the dashboard's switch, each change requests an automatic publish,
	// which once auto sync is back on sends the changes it held.
	require.Equal(t, string(plugindelivery.ProjectPublicationEmissionDisabled), turnedOn.PublicationRequest)
	require.Equal(t, "best_effort_requested", turnedOn.PublishSignal)
	require.Equal(t, []pluginPublishSignal{
		{projectID: fixture.project.ID, userID: fixture.principal.UserID},
		{projectID: fixture.project.ID, userID: fixture.principal.UserID},
	}, signaler.recorded(), "an ordinary publish for the change off and the change on; the replay signals nothing")
	again, err := fixture.service.GetPlugin(ctx, fixture.principal, GetPluginInput{ProjectID: fixture.project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Equal(t, before.Plugin, again.Plugin)
}

// Turning auto sync off is refused, with nothing written, while the plugin
// has changes that haven't been synced. The refusal tells the agent to sync
// first.
func TestSetPluginAutoSyncRefusesTurningOffWithUnsyncedChanges(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedPluginMetadataFixture(t, "platform_mcp_set_plugin_auto_sync_unsynced")
	plugin := seedPlugin(t, ctx, fixture.conn, fixture.principal.OrganizationID, fixture.project.ID, "Unsynced", "unsynced")
	fixture.service.WithAutoSyncGuard(&stubAutoSyncGuard{unsynced: map[uuid.UUID]bool{plugin.ID: true}})
	beforeAudit, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)

	payload := invokePluginMetadataRefusal(t, fixture.descriptor(t, operationSetPluginAutoSync), map[string]any{
		"project_id": fixture.project.ID.String(), "plugin": plugin.ID.String(), "auto_sync_enabled": false,
		"idempotency_key": "refused-off", "confirmed": true,
	}, ContextWithPrincipal(ctx, fixture.principal))
	var refusal pluginMetadataRefusal
	require.NoError(t, json.Unmarshal([]byte(payload), &refusal))
	require.Equal(t, pluginMetadataCodeUnsyncedChanges, refusal.Code)
	require.Contains(t, refusal.Message, "republish_plugin")
	require.Contains(t, refusal.Message, "fresh=true")

	got, err := fixture.service.GetPlugin(ctx, fixture.principal, GetPluginInput{ProjectID: fixture.project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Equal(t, new(true), got.Plugin.AutoSyncEnabled, "a refused change leaves auto sync on")
	afterAudit, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAudit, afterAudit)
	requireNoOperationReceipt(t, ctx, fixture.conn, fixture.principal, fixture.project, operationSetPluginAutoSync, "refused-off")
	require.Empty(t, fixture.budget.keys, "a refused change is checked before the allowance is charged")
}

// racingAutoSyncGuard passes the early check and refuses the one in the
// write's transaction, as the plugins service does when a change to the plugin
// commits between the two.
type racingAutoSyncGuard struct{}

func (racingAutoSyncGuard) RequireAutoSyncCanTurnOff(context.Context, string, uuid.UUID, uuid.UUID) error {
	return nil
}

func (racingAutoSyncGuard) RequireAutoSyncCanTurnOffInTransaction(context.Context, pgx.Tx, string, uuid.UUID, uuid.UUID) error {
	return unsyncedChangesRefusal()
}

// The check in the write's transaction is the one that decides: a change that
// lands after the early check still refuses turning auto sync off, with
// nothing written.
func TestSetPluginAutoSyncRefusesAChangeThatLandsAfterTheEarlyCheck(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedPluginMetadataFixture(t, "platform_mcp_set_plugin_auto_sync_race")
	plugin := seedPlugin(t, ctx, fixture.conn, fixture.principal.OrganizationID, fixture.project.ID, "Raced", "raced")
	fixture.service.WithAutoSyncGuard(racingAutoSyncGuard{})
	beforeAudit, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)

	_, err = fixture.service.SetPluginAutoSync(ctx, fixture.principal, SetPluginAutoSyncInput{
		ProjectID: fixture.project.ID.String(), Plugin: plugin.ID.String(), AutoSyncEnabled: false, IdempotencyKey: "raced-off", Confirmed: true,
	})
	var refusal *PluginMetadataMutationError
	require.ErrorAs(t, err, &refusal)
	require.Equal(t, pluginMetadataCodeUnsyncedChanges, refusal.Code)
	require.ErrorIs(t, refusal, plugindelivery.ErrPluginHasUnsyncedChanges)

	got, err := fixture.service.GetPlugin(ctx, fixture.principal, GetPluginInput{ProjectID: fixture.project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Equal(t, new(true), got.Plugin.AutoSyncEnabled, "a refused change leaves auto sync on")
	afterAudit, err := audittest.AuditLogCountByAction(ctx, fixture.conn, audit.ActionPluginUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeAudit, afterAudit)
	requireNoOperationReceipt(t, ctx, fixture.conn, fixture.principal, fixture.project, operationSetPluginAutoSync, "raced-off")
}

// The plugins service's own check refuses through the tool: a stored
// fingerprint that no longer matches the plugin's live state is an unsynced
// change, and a project with no marketplace has nothing to strand.
func TestSetPluginAutoSyncUsesThePluginsServiceCheck(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedPluginMetadataFixture(t, "platform_mcp_set_plugin_auto_sync_service")
	engine := authz.NewEngine(testenv.NewLogger(t), fixture.conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	management := plugindelivery.NewService(testenv.NewLogger(t), testenv.NewTracerProvider(t), fixture.conn, &sessions.Manager{}, nil, engine, audit.NewLogger(), nil, "test", "https://example.test", nil, nil)
	fixture.service.WithAutoSyncGuard(management)
	plugin := seedPlugin(t, ctx, fixture.conn, fixture.principal.OrganizationID, fixture.project.ID, "Checked", "checked")
	off := func(key string) error {
		_, err := fixture.service.SetPluginAutoSync(ctx, fixture.principal, SetPluginAutoSyncInput{
			ProjectID: fixture.project.ID.String(), Plugin: plugin.ID.String(), AutoSyncEnabled: false, IdempotencyKey: key, Confirmed: true,
		})
		return err
	}

	seedMarketplaceConnection(t, ctx, fixture.conn, fixture.project.ID, []byte(`{"checked":"published-before-the-latest-change"}`))
	var refusal *PluginMetadataMutationError
	require.ErrorAs(t, off("off-unsynced"), &refusal)
	require.Equal(t, pluginMetadataCodeUnsyncedChanges, refusal.Code)
	require.ErrorIs(t, refusal, plugindelivery.ErrPluginHasUnsyncedChanges)

	require.NoError(t, pluginsrepo.New(fixture.conn).DeleteGitHubConnection(ctx, fixture.project.ID))
	require.NoError(t, off("off-no-marketplace"))
	got, err := fixture.service.GetPlugin(ctx, fixture.principal, GetPluginInput{ProjectID: fixture.project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Equal(t, new(false), got.Plugin.AutoSyncEnabled)
}

// The listed input schemas carry auto sync: optional on create_plugin, where
// leaving it out keeps it on, and required on set_plugin_auto_sync.
func TestPluginAutoSyncInputSchemas(t *testing.T) {
	t.Parallel()
	_, fixture := seedPluginMetadataFixture(t, "platform_mcp_plugin_auto_sync_schemas")
	schema := func(name string) (properties map[string]json.RawMessage, required []string) {
		t.Helper()
		var decoded struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		require.NoError(t, json.Unmarshal(fixture.descriptor(t, name).InputSchema, &decoded))
		return decoded.Properties, decoded.Required
	}

	properties, required := schema(operationCreatePlugin)
	require.Contains(t, properties, "auto_sync_enabled")
	require.NotContains(t, required, "auto_sync_enabled")

	properties, required = schema(operationSetPluginAutoSync)
	require.ElementsMatch(t, []string{"project_id", "plugin", "auto_sync_enabled", "idempotency_key", "confirmed"}, required)
	require.Len(t, properties, len(required))
}

func TestSetPluginAutoSyncRequiresConfirmationAndPluginWrite(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedPluginMetadataFixture(t, "platform_mcp_set_plugin_auto_sync_refusals")
	guard := &stubAutoSyncGuard{}
	fixture.service.WithAutoSyncGuard(guard)
	plugin := seedPlugin(t, ctx, fixture.conn, fixture.principal.OrganizationID, fixture.project.ID, "Guarded", "guarded")
	descriptor := fixture.descriptor(t, operationSetPluginAutoSync)
	arguments := func(confirmed bool) map[string]any {
		return map[string]any{
			"project_id": fixture.project.ID.String(), "plugin": plugin.ID.String(), "auto_sync_enabled": false,
			"idempotency_key": uuid.NewString(), "confirmed": confirmed,
		}
	}

	var refusal pluginMetadataRefusal
	require.NoError(t, json.Unmarshal([]byte(invokePluginMetadataRefusal(t, descriptor, arguments(false), ContextWithPrincipal(ctx, fixture.principal))), &refusal))
	require.Equal(t, "confirmation_required", refusal.Code)
	require.Empty(t, fixture.budget.keys, "an unconfirmed change is not charged")

	reader := ContextWithPrincipal(authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, fixture.project.ID.String())}), fixture.principal)
	var denied externalAuthorizationRefusal
	require.NoError(t, json.Unmarshal([]byte(invokePluginMetadataRefusal(t, descriptor, arguments(true), reader)), &denied))
	require.Equal(t, "permission_denied", denied.Code)
	require.Equal(t, string(authz.ScopePluginWrite), denied.RequiredScope)

	require.Empty(t, guard.checks(), "nothing was checked or written for a refused call")
	got, err := fixture.service.GetPlugin(ctx, fixture.principal, GetPluginInput{ProjectID: fixture.project.ID.String(), Plugin: plugin.ID.String()})
	require.NoError(t, err)
	require.Equal(t, new(true), got.Plugin.AutoSyncEnabled)
}

// Without the turn-off check composed, set_plugin_auto_sync answers with the
// same unavailable refusal as the other metadata writes rather than changing
// auto sync without the dashboard's safeguard.
func TestSetPluginAutoSyncIsUnavailableWithoutTheTurnOffCheck(t *testing.T) {
	t.Parallel()
	ctx, fixture := seedPluginMetadataFixture(t, "platform_mcp_set_plugin_auto_sync_unavailable")
	fixture.service.WithAutoSyncGuard(nil)
	require.True(t, fixture.service.metadataMutationValid())
	require.False(t, fixture.service.autoSyncMutationValid())

	payload := invokePluginMetadataRefusal(t, fixture.descriptor(t, operationSetPluginAutoSync), map[string]any{
		"project_id": fixture.project.ID.String(), "plugin": "anything", "auto_sync_enabled": false,
		"idempotency_key": uuid.NewString(), "confirmed": true,
	}, ContextWithPrincipal(ctx, fixture.principal))
	require.Contains(t, payload, pluginMetadataFeature)
	require.Contains(t, payload, unavailableCode)
}

// republish_plugin is the dashboard's Sync changes: a plugin whose auto sync
// is off keeps its changes until someone syncs, so a republish must not stop
// at already_current while one has changes waiting, and what it requests also
// publishes them.
func TestRepublishPluginSyncsChangesWaitingOnPausedPlugins(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_republish_paused")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Current", "current")
	seedPausedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Waiting", "waiting")
	evidence := slugPublicationEvidence{"current": new(true), "waiting": new(false)}
	signaler := &recordingPluginPublishSignaler{}
	service := testPluginTargets(conn).WithPublicationEvidence(evidence).
		WithRepublish(plugindelivery.PublicationRequests{Enabled: false}, signaler, testOperationBudget())

	got, err := service.RepublishPlugin(ctx, principal, RepublishPluginInput{ProjectID: project.ID.String(), Plugin: "current", Confirmed: true, IdempotencyKey: "current-with-waiting"})
	require.NoError(t, err)
	require.Equal(t, PluginRepublishEnqueued, got.Outcome, "a paused plugin's waiting changes need the sync")
	require.Equal(t, []pluginPublishSignal{{projectID: project.ID, userID: principal.UserID, sync: true}}, signaler.recorded())
	require.False(t, got.PublicationEvidence.AwaitingManualSync, "the named plugin's auto sync is on")

	waiting, err := service.RepublishPlugin(ctx, principal, RepublishPluginInput{ProjectID: project.ID.String(), Plugin: "waiting", Confirmed: true, IdempotencyKey: "waiting"})
	require.NoError(t, err)
	require.Equal(t, PluginRepublishEnqueued, waiting.Outcome)
	require.True(t, waiting.PublicationEvidence.AwaitingManualSync, "the evidence read after the request reports the plugin as it stands")

	// Once the paused plugin is current too, there is nothing to sync.
	evidence["waiting"] = new(true)
	current, err := service.RepublishPlugin(ctx, principal, RepublishPluginInput{ProjectID: project.ID.String(), Plugin: "current", Confirmed: true, IdempotencyKey: "all-current"})
	require.NoError(t, err)
	require.Equal(t, PluginRepublishAlreadyCurrent, current.Outcome)
	require.Len(t, signaler.recorded(), 2)
}
