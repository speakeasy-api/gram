package plugins_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/plugins/naming"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// orgWildcardPrincipal is the assignment principal that reaches every member of
// an organization, so the device-agent view lists a non-default project.
const orgWildcardPrincipal = "*"

// pushedMarketplaceName returns the name the manifests in the last push carry,
// failing when the Claude, Cursor, Codex, and Copilot manifests disagree.
func pushedMarketplaceName(t *testing.T, mock *mockGitHubPublisher) string {
	t.Helper()

	paths := []string{
		".claude-plugin/marketplace.json",
		".cursor-plugin/marketplace.json",
		".agents/plugins/marketplace.json",
		"marketplace.json",
	}
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		raw, ok := mock.lastPushedFiles[path]
		require.True(t, ok, "missing %s in pushed files", path)
		var manifest struct {
			Name string `json:"name"`
		}
		require.NoError(t, json.Unmarshal(raw, &manifest))
		names = append(names, manifest.Name)
	}
	for i, name := range names {
		require.Equal(t, names[0], name, "%s disagrees with %s", paths[i], paths[0])
	}
	return names[0]
}

// recordedMarketplaceName reads back the published marketplace name the
// project's connection records.
func recordedMarketplaceName(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) string {
	t.Helper()

	connection, err := pluginsrepo.New(conn).GetGitHubConnection(ctx, projectID)
	require.NoError(t, err)
	return naming.PublishedMarketplaceName(connection.PublishedHooksConfig)
}

// agentMarketplaceName returns the marketplace name the device-agent endpoint
// emits for the project, through the query and view GetPlugins uses.
func agentMarketplaceName(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, projectID uuid.UUID) string {
	t.Helper()

	connection, err := pluginsrepo.New(conn).GetGitHubConnection(ctx, projectID)
	require.NoError(t, err)
	require.True(t, connection.MarketplaceToken.Valid)

	rows, err := agentrepo.New(conn).GetAgentPluginSet(ctx, agentrepo.GetAgentPluginSetParams{
		OrganizationID: orgID,
		PrincipalUrns:  []string{orgWildcardPrincipal},
	})
	require.NoError(t, err)
	view := mv.BuildAgentPluginsView(rows, func(token string) string {
		return "https://app.getgram.ai/marketplace/" + token + ".git"
	})
	for _, marketplace := range view.Marketplaces {
		if strings.HasSuffix(marketplace.URL, "/"+connection.MarketplaceToken.String+".git") {
			return marketplace.Name
		}
	}
	require.FailNow(t, "project marketplace missing from the device-agent view")
	return ""
}

// requireMarketplaceNameEverywhere asserts every surface agrees on want for the
// project in ctx: the manifests last pushed, the recorded published name, the
// dashboard settings, the device-agent endpoint, and the Codex install script.
func requireMarketplaceNameEverywhere(t *testing.T, ctx context.Context, ti *testInstance, mock *mockGitHubPublisher, want string) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	require.Equal(t, want, pushedMarketplaceName(t, mock), "pushed marketplace manifests")
	require.Equal(t, want, recordedMarketplaceName(t, ctx, ti.conn, projectID), "recorded published name")

	settings, err := ti.service.GetMarketplaceSettings(ctx, &gen.GetMarketplaceSettingsPayload{})
	require.NoError(t, err)
	require.Equal(t, want, settings.EffectiveName, "dashboard effective name")

	require.Equal(t, want, agentMarketplaceName(t, ctx, ti.conn, authCtx.ActiveOrganizationID, projectID), "device-agent marketplace name")

	_, body, err := ti.service.DownloadCodexInstallScript(ctx, &gen.DownloadCodexInstallScriptPayload{})
	require.NoError(t, err)
	script, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	require.Contains(t, string(script), fmt.Sprintf("MARKETPLACE_KEY=%q", want), "codex install script")
}

// renameOrganization changes the org's display name through the WorkOS sync
// query, the path a real org rename takes.
func renameOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, name string) {
	t.Helper()

	q := orgrepo.New(conn)
	org, err := q.GetOrganizationMetadata(ctx, orgID)
	require.NoError(t, err)
	domains := org.VerifiedDomains
	if domains == nil {
		domains = []string{}
	}
	_, err = q.UpdateOrganizationMetadataFromWorkOS(ctx, orgrepo.UpdateOrganizationMetadataFromWorkOSParams{
		Name:              name,
		WorkosID:          org.WorkosID,
		WorkosUpdatedAt:   org.WorkosUpdatedAt,
		WorkosLastEventID: org.WorkosLastEventID,
		VerifiedDomains:   domains,
		ID:                orgID,
	})
	require.NoError(t, err)
}

func organizationName(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string) string {
	t.Helper()

	name, err := pluginsrepo.New(conn).GetOrganizationName(ctx, orgID)
	require.NoError(t, err)
	return name
}

// withProject scopes ctx's auth context to project, as a session in that
// project would be.
func withProject(t *testing.T, ctx context.Context, projectID uuid.UUID, projectSlug string) context.Context {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	scoped := *authCtx
	scoped.ProjectID = &projectID
	scoped.ProjectSlug = &projectSlug
	return contextvalues.SetAuthContext(ctx, &scoped)
}

// createNewerProject adds a project to ctx's org. It is newer than the test's
// original project, so it is not the org's default project.
func createNewerProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool, slug string) projectsrepo.Project {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	return project
}

func assignToOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID, pluginID string) {
	t.Helper()

	_, err := pluginsrepo.New(conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
		PluginID:       uuid.MustParse(pluginID),
		OrganizationID: orgID,
		PrincipalUrn:   orgWildcardPrincipal,
	})
	require.NoError(t, err)
}

// rewritePublishedHooksConfig applies edit to the project's stored published
// hooks config snapshot and writes it back, keeping the rest of the connection.
func rewritePublishedHooksConfig(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, edit func(fields map[string]json.RawMessage)) {
	t.Helper()

	q := pluginsrepo.New(conn)
	current, err := q.GetGitHubConnection(ctx, projectID)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(current.PublishedHooksConfig, &fields))
	edit(fields)
	rewritten, err := json.Marshal(fields)
	require.NoError(t, err)
	_, err = q.UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID:                projectID,
		InstallationID:           current.InstallationID,
		RepoOwner:                current.RepoOwner,
		RepoName:                 current.RepoName,
		MarketplaceToken:         current.MarketplaceToken,
		PublishedMcpFingerprints: current.PublishedMcpFingerprints,
		PublishedHooksVersion:    current.PublishedHooksVersion,
		PublishedHooksConfig:     rewritten,
	})
	require.NoError(t, err)
}

func republish(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()

	_, err := ti.service.PublishPlugins(ctx, &gen.PublishPluginsPayload{})
	require.NoError(t, err)
}

func TestMarketplaceName_FirstPublishUsesComputedName(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	marketplaceRenameFixture(t, ctx, ti, "Freeze First Publish")

	requireMarketplaceNameEverywhere(t, ctx, ti, mock, defaultMarketplaceNameForTest(t, ctx, ti))
}

func TestMarketplaceName_OrgRenameKeepsPublishedName(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	orgID, _ := marketplaceRenameFixture(t, ctx, ti, "Freeze Org Rename")
	published := defaultMarketplaceNameForTest(t, ctx, ti)

	renameOrganization(t, ctx, ti.conn, orgID, "Renamed Freeze Org")
	require.NotEqual(t, published, defaultMarketplaceNameForTest(t, ctx, ti), "the rename must move the computed name")
	republish(t, ctx, ti)

	requireMarketplaceNameEverywhere(t, ctx, ti, mock, published)
	settings, err := ti.service.GetMarketplaceSettings(ctx, &gen.GetMarketplaceSettingsPayload{})
	require.NoError(t, err)
	require.Equal(t, published, settings.DefaultName, "the dashboard default must be the frozen name")
}

func TestMarketplaceName_ProjectSlugChangeKeepsPublishedName(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	project := createNewerProject(t, ctx, ti.conn, "freeze-beta")
	ctx = withProject(t, ctx, project.ID, project.Slug)
	orgID, pluginID := marketplaceRenameFixture(t, ctx, ti, "Freeze Slug Change")
	assignToOrganization(t, ctx, ti.conn, orgID, pluginID)

	published := naming.MarketplaceName(organizationName(t, ctx, ti.conn, orgID), "freeze-beta", false)
	requireMarketplaceNameEverywhere(t, ctx, ti, mock, published)

	require.NoError(t, testrepo.New(ti.conn).SetProjectSlugFixture(ctx, testrepo.SetProjectSlugFixtureParams{
		Slug: "freeze-gamma",
		ID:   project.ID,
	}))
	ctx = withProject(t, ctx, project.ID, "freeze-gamma")
	republish(t, ctx, ti)

	requireMarketplaceNameEverywhere(t, ctx, ti, mock, published)
}

func TestMarketplaceName_DefaultProjectDeletionKeepsPublishedName(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	oldestProjectID := *authCtx.ProjectID

	project := createNewerProject(t, ctx, ti.conn, "freeze-beta")
	ctx = withProject(t, ctx, project.ID, project.Slug)
	orgID, _ := marketplaceRenameFixture(t, ctx, ti, "Freeze Default Change")
	orgName := organizationName(t, ctx, ti.conn, orgID)
	published := naming.MarketplaceName(orgName, "freeze-beta", false)
	require.Equal(t, published, pushedMarketplaceName(t, mock))

	// Deleting the oldest project makes this one the org's default, which moves
	// its computed name to the bare org-derived one.
	_, err := projectsrepo.New(ti.conn).DeleteProject(ctx, oldestProjectID)
	require.NoError(t, err)
	require.NotEqual(t, published, naming.MarketplaceName(orgName, "freeze-beta", true))
	republish(t, ctx, ti)

	requireMarketplaceNameEverywhere(t, ctx, ti, mock, published)
}

func TestMarketplaceName_OverrideRenamesThenStaysFrozen(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	orgID, _ := marketplaceRenameFixture(t, ctx, ti, "Freeze Override")

	name := "team-tools"
	result, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{MarketplaceName: &name})
	require.NoError(t, err)
	require.True(t, result.Republished)
	requireMarketplaceNameEverywhere(t, ctx, ti, mock, "team-tools")

	renameOrganization(t, ctx, ti.conn, orgID, "Renamed Override Org")
	republish(t, ctx, ti)

	requireMarketplaceNameEverywhere(t, ctx, ti, mock, "team-tools")
	settings, err := ti.service.GetMarketplaceSettings(ctx, &gen.GetMarketplaceSettingsPayload{})
	require.NoError(t, err)
	require.Equal(t, defaultMarketplaceNameForTest(t, ctx, ti), settings.DefaultName,
		"a published override is not the project's default; clearing it returns to the computed name")
}

func TestMarketplaceName_ClearingPublishedOverrideReturnsToComputedName(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)

	orgID, _ := marketplaceRenameFixture(t, ctx, ti, "Freeze Clear Published")
	name := "team-tools"
	_, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{MarketplaceName: &name})
	require.NoError(t, err)

	renameOrganization(t, ctx, ti.conn, orgID, "Renamed Clear Org")
	computed := defaultMarketplaceNameForTest(t, ctx, ti)

	// The dashboard shows the name clearing will publish under before the save.
	before, err := ti.service.GetMarketplaceSettings(ctx, &gen.GetMarketplaceSettingsPayload{})
	require.NoError(t, err)
	require.Equal(t, computed, before.DefaultName)

	empty := ""
	result, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{MarketplaceName: &empty})
	require.NoError(t, err)
	require.True(t, result.Republished)
	require.Nil(t, result.Settings.MarketplaceName)
	require.Equal(t, computed, result.Settings.EffectiveName)
	requireMarketplaceNameEverywhere(t, ctx, ti, mock, computed)

	// The name it returned to is frozen like any other published name.
	renameOrganization(t, ctx, ti.conn, orgID, "Renamed Again Org")
	republish(t, ctx, ti)
	requireMarketplaceNameEverywhere(t, ctx, ti, mock, computed)
}

func TestMarketplaceName_ClearingUnpublishedOverrideKeepsPublishedName(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	orgID, _ := marketplaceRenameFixture(t, ctx, ti, "Freeze Clear Unpublished")
	published := defaultMarketplaceNameForTest(t, ctx, ti)

	// An override saved without its republish landing: the repo still holds
	// the published name.
	_, err := pluginsrepo.New(ti.conn).UpsertMarketplaceSettings(ctx, pluginsrepo.UpsertMarketplaceSettingsParams{
		ProjectID:               *authCtx.ProjectID,
		SetMarketplaceName:      true,
		MarketplaceName:         conv.ToPGText("team-tools"),
		SetObservabilityEnabled: false,
		ObservabilityEnabled:    pgtype.Bool{},
	})
	require.NoError(t, err)
	renameOrganization(t, ctx, ti.conn, orgID, "Renamed Unpublished Org")

	before, err := ti.service.GetMarketplaceSettings(ctx, &gen.GetMarketplaceSettingsPayload{})
	require.NoError(t, err)
	require.Equal(t, "team-tools", before.EffectiveName)
	require.Equal(t, published, before.DefaultName, "the live published name is still the project's default")

	empty := ""
	result, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{MarketplaceName: &empty})
	require.NoError(t, err)
	require.True(t, result.Republished)
	requireMarketplaceNameEverywhere(t, ctx, ti, mock, published)
}

func TestMarketplaceName_SnapshotWithoutPublishedNameKeepsTodaysName(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	orgID, _ := marketplaceRenameFixture(t, ctx, ti, "Freeze Legacy Snapshot")
	today := defaultMarketplaceNameForTest(t, ctx, ti)

	// Rewrite the snapshot to one written before published names were recorded,
	// whose hooks marketplace_name lags the live manifests (as a hooks subtree
	// carried by the rollout gate can).
	rewritePublishedHooksConfig(t, ctx, ti.conn, projectID, func(fields map[string]json.RawMessage) {
		delete(fields, naming.PublishedMarketplaceNameKey)
		fields["marketplace_name"] = json.RawMessage(`"stale-speakeasy"`)
	})
	require.Empty(t, recordedMarketplaceName(t, ctx, ti.conn, projectID))

	settings, err := ti.service.GetMarketplaceSettings(ctx, &gen.GetMarketplaceSettingsPayload{})
	require.NoError(t, err)
	require.Equal(t, today, settings.EffectiveName)
	require.Equal(t, today, agentMarketplaceName(t, ctx, ti.conn, orgID, projectID))

	// The hourly rollout sweep finds nothing to publish and records the name
	// the repo already carries, without a commit.
	mock.pushFilesCalled = false
	result, err := ti.service.PublishProject(ctx, plugins.PublishProjectInput{
		ProjectID:       projectID,
		CreatedByUserID: authCtx.UserID,
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	})
	require.NoError(t, err)
	require.True(t, result.Skipped)
	require.False(t, mock.pushFilesCalled)
	require.Equal(t, today, recordedMarketplaceName(t, ctx, ti.conn, projectID))

	renameOrganization(t, ctx, ti.conn, orgID, "Renamed Legacy Org")
	republish(t, ctx, ti)
	requireMarketplaceNameEverywhere(t, ctx, ti, mock, today)
}

func TestMarketplaceName_UnchangedPublishDoesNotRecordOverride(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID

	orgID, _ := marketplaceRenameFixture(t, ctx, ti, "Freeze Legacy Override")
	name := "team-tools"
	_, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{MarketplaceName: &name})
	require.NoError(t, err)

	// The override published before published names were recorded.
	rewritePublishedHooksConfig(t, ctx, ti.conn, projectID, func(fields map[string]json.RawMessage) {
		delete(fields, naming.PublishedMarketplaceNameKey)
	})

	// The rollout sweep skips the unchanged project and leaves the override
	// unrecorded: the override already fixes the name.
	mock.pushFilesCalled = false
	result, err := ti.service.PublishProject(ctx, plugins.PublishProjectInput{
		ProjectID:       projectID,
		CreatedByUserID: authCtx.UserID,
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	})
	require.NoError(t, err)
	require.True(t, result.Skipped)
	require.False(t, mock.pushFilesCalled)
	require.Empty(t, recordedMarketplaceName(t, ctx, ti.conn, projectID))

	// An admin clears the override while that sweep runs, so the clear finds
	// no recorded name to forget, and its republish does not land. A recorded
	// override would now freeze the cleared name.
	_, err = pluginsrepo.New(ti.conn).UpsertMarketplaceSettings(ctx, pluginsrepo.UpsertMarketplaceSettingsParams{
		ProjectID:               projectID,
		SetMarketplaceName:      true,
		MarketplaceName:         pgtype.Text{},
		SetObservabilityEnabled: false,
		ObservabilityEnabled:    pgtype.Bool{},
	})
	require.NoError(t, err)
	computed := defaultMarketplaceNameForTest(t, ctx, ti)
	settings, err := ti.service.GetMarketplaceSettings(ctx, &gen.GetMarketplaceSettingsPayload{})
	require.NoError(t, err)
	require.Equal(t, computed, settings.EffectiveName)
	require.Equal(t, computed, agentMarketplaceName(t, ctx, ti.conn, orgID, projectID))

	// The next sweep publishes the computed name the manifests now lag.
	result, err = ti.service.PublishProject(ctx, plugins.PublishProjectInput{
		ProjectID:       projectID,
		CreatedByUserID: authCtx.UserID,
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	})
	require.NoError(t, err)
	require.False(t, result.Skipped)
	requireMarketplaceNameEverywhere(t, ctx, ti, mock, computed)
}

func TestMarketplaceName_UnchangedEligibleOrgSkipsWithoutHooksRegeneration(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	features := &feature.InMemory{}
	ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, mock, features)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	features.SetFlagPayload(feature.FlagHooksRollout, authCtx.ActiveOrganizationID, []byte(`{"version": 9999}`))

	orgID, _ := marketplaceRenameFixture(t, ctx, ti, "Freeze Unchanged Eligible")
	require.NotEmpty(t, recordedMarketplaceName(t, ctx, ti.conn, *authCtx.ProjectID))
	hooksKeysBefore := countPluginHooksKeys(t, ctx, ti.conn, orgID)

	mock.pushFilesCalled = false
	result, err := ti.service.PublishProject(ctx, plugins.PublishProjectInput{
		ProjectID:       *authCtx.ProjectID,
		CreatedByUserID: authCtx.UserID,
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	})
	require.NoError(t, err)
	require.True(t, result.Skipped, "the recorded name must not read as a hooks change")
	require.False(t, mock.pushFilesCalled)
	require.Equal(t, hooksKeysBefore, countPluginHooksKeys(t, ctx, ti.conn, orgID), "no hooks regeneration")
}

func TestMarketplaceName_GatedOrgRecordsNameOnCarriedHooks(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	// Empty provider: no clearance payload, so the org is held at its published
	// hooks and every later publish carries the hooks subtree.
	ctx, ti := newTestPluginsServiceWithGitHubAndFeatures(t, mock, &feature.InMemory{})
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	orgID, _ := marketplaceRenameFixture(t, ctx, ti, "Freeze Gated")
	published := defaultMarketplaceNameForTest(t, ctx, ti)
	hooksKeysBefore := countPluginHooksKeys(t, ctx, ti.conn, orgID)

	renameOrganization(t, ctx, ti.conn, orgID, "Renamed Gated Org")
	republish(t, ctx, ti)
	require.Equal(t, published, pushedMarketplaceName(t, mock))
	require.Equal(t, published, recordedMarketplaceName(t, ctx, ti.conn, *authCtx.ProjectID))

	name := "team-tools"
	result, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{MarketplaceName: &name})
	require.NoError(t, err)
	require.True(t, conv.PtrValOr(result.HooksUpdateDeferred, false))
	require.Equal(t, hooksKeysBefore, countPluginHooksKeys(t, ctx, ti.conn, orgID), "the hooks subtree stays carried")
	require.Equal(t, "team-tools", pushedMarketplaceName(t, mock))
	require.Equal(t, "team-tools", recordedMarketplaceName(t, ctx, ti.conn, *authCtx.ProjectID),
		"a carried hooks snapshot still records the name the manifests were pushed under")
}

func TestMarketplaceName_ObservabilityDisabledProjectFreezesAndSkips(t *testing.T) {
	t.Parallel()

	mock := &mockGitHubPublisher{}
	ctx, ti := newTestPluginsServiceWithGitHub(t, mock)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	orgID, _ := marketplaceRenameFixture(t, ctx, ti, "Freeze Observability Off")
	published := defaultMarketplaceNameForTest(t, ctx, ti)

	disabled := false
	result, err := ti.service.UpdateMarketplaceSettings(ctx, &gen.UpdateMarketplaceSettingsPayload{ObservabilityEnabled: &disabled})
	require.NoError(t, err)
	require.True(t, result.Republished)
	require.Equal(t, published, recordedMarketplaceName(t, ctx, ti.conn, *authCtx.ProjectID),
		"a project without the observability plugin still records its published name")

	mock.pushFilesCalled = false
	outcome, err := ti.service.PublishProject(ctx, plugins.PublishProjectInput{
		ProjectID:       *authCtx.ProjectID,
		CreatedByUserID: authCtx.UserID,
		CommitMessage:   "Update plugin packages",
		SkipIfUnchanged: true,
	})
	require.NoError(t, err)
	require.True(t, outcome.Skipped, "the recorded name must not read as a leftover hooks subtree")
	require.False(t, mock.pushFilesCalled)

	renameOrganization(t, ctx, ti.conn, orgID, "Renamed Observability Off Org")
	republish(t, ctx, ti)
	require.Equal(t, published, pushedMarketplaceName(t, mock))
	require.Equal(t, published, agentMarketplaceName(t, ctx, ti.conn, orgID, *authCtx.ProjectID))
	settings, err := ti.service.GetMarketplaceSettings(ctx, &gen.GetMarketplaceSettingsPayload{})
	require.NoError(t, err)
	require.Equal(t, published, settings.EffectiveName)
}
