package access

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	publicationv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	gen "github.com/speakeasy-api/gram/server/gen/access"
	plugingen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	endpointrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestService_UpdateRole_DeliversServerToExistingAudiencePlugin(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	repoName := strings.ToLower(f.ac.OrganizationSlug + "-" + *f.ac.ProjectSlug + "-plugins")
	var files map[string][]byte
	f.github.On("CreateRepo", mock.Anything, int64(12345), "test-org", repoName, true).Return(nil)
	f.github.On("PushFiles", mock.Anything, int64(12345), "test-org", repoName, "main", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			pushed, ok := args.Get(6).(map[string][]byte)
			require.True(t, ok)
			files = pushed
		}).Return("test-commit", nil)
	f.github.On("GetRepoFiles", mock.Anything, int64(12345), "test-org", repoName, "main").
		Return(func() map[string][]byte { return files }, nil).Maybe()
	f.github.On("GetFileContent", mock.Anything, int64(12345), "test-org", repoName, "main", f.plugin.Slug+"/.claude-plugin/plugin.json").Return([]byte(nil), ghclient.ErrFileNotFound).Maybe()
	f.github.On("HasDirectCollaborator", mock.Anything, int64(12345), "test-org", repoName).Return(true, nil).Maybe()
	// Establish an existing marketplace through its public publishing API.
	_, err := f.pluginService.PublishPlugins(ctx, &plugingen.PublishPluginsPayload{})
	require.NoError(t, err)
	beforeEvents, err := testrepo.New(f.ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	beforeIDs := make(map[int64]bool, len(beforeEvents))
	for _, event := range beforeEvents {
		beforeIDs[event.ID] = true
	}
	f.patch(t, ctx, f.roleID, true)
	require.Equal(t, f.serverID.String(), *f.read(t, ctx, 1).Servers[0].McpServerID)
	events, err := testrepo.New(f.ti.conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	refreshRequested := false
	for _, event := range events {
		if beforeIDs[event.ID] || event.Topic != "gram.plugins.v1.PublicationRequested" {
			continue
		}
		request := &publicationv1.PublicationRequested{}
		require.NoError(t, proto.Unmarshal(event.Message, request))
		if request.GetOrganizationId() == f.ac.ActiveOrganizationID && request.GetProjectId() == f.ac.ProjectID.String() {
			refreshRequested = true
		}
	}
	require.True(t, refreshRequested, "content delivery requests a refresh of the affected marketplace")
	// Invoke the public publisher directly; this does not exercise outbox consumption.
	publisher := plugins.NewPublisher(testenv.NewLogger(t), f.ti.conn, f.ti.service.audit, f.githubConfig, "test", "https://example.com", nil)
	result, err := publisher.PublishProject(ctx, plugins.PublishProjectInput{ProjectID: *f.ac.ProjectID, CreatedByUserID: f.ac.UserID, CommitMessage: "Refresh role delivery", SkipIfUnchanged: true})
	require.NoError(t, err)
	require.False(t, result.Skipped)
	var artifact struct {
		Servers map[string]struct {
			URL string `json:"url"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(files[f.plugin.Slug+"/.mcp.json"], &artifact))
	require.Len(t, artifact.Servers, 1)
	for _, server := range artifact.Servers {
		require.Equal(t, "https://example.com/mcp/delivery-endpoint", server.URL)
	}
}

func TestService_UpdateRole_RevokingGrantRemovesManualPluginContent(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	serverIDString := f.serverID.String()
	manual, err := f.pluginService.AddPluginServer(ctx, &plugingen.AddPluginServerPayload{PluginID: f.plugin.ID, McpServerID: &serverIDString, Policy: "optional"})
	require.NoError(t, err)
	f.patch(t, ctx, f.roleID, true)
	retained := f.read(t, ctx, 1)
	require.Equal(t, manual.ID, retained.Servers[0].ID)
	require.Equal(t, "optional", retained.Servers[0].Policy)
	f.patch(t, ctx, f.roleID, false)
	f.read(t, ctx, 0)
}

func TestService_UpdateRole_RetainsPluginServerWhileAnotherRoleGrantsAccess(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	otherRoleID := seedRole(t, ctx, f.ti.conn, f.ac.ActiveOrganizationID, mockRole("role_backup", "Backup", "backup", ""))
	_, err := f.pluginService.SetPluginAssignments(ctx, &plugingen.SetPluginAssignmentsPayload{PluginID: f.plugin.ID, PrincipalUrns: []string{"role:organization:" + f.roleID, "role:organization:" + otherRoleID}})
	require.NoError(t, err)
	f.patch(t, ctx, f.roleID, true)
	shared := f.read(t, ctx, 1).Servers[0].ID
	f.patch(t, ctx, otherRoleID, true)
	f.patch(t, ctx, f.roleID, false)
	require.Equal(t, shared, f.read(t, ctx, 1).Servers[0].ID)
	f.patch(t, ctx, otherRoleID, false)
	f.read(t, ctx, 0)
}

func TestService_UpdateRole_ExplicitPluginRemovalSurvivesReplayUntilNewGrant(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	f.patch(t, ctx, f.roleID, true)
	removedID := f.read(t, ctx, 1).Servers[0].ID
	require.NoError(t, f.pluginService.RemovePluginServer(ctx, &plugingen.RemovePluginServerPayload{ID: removedID, PluginID: f.plugin.ID}))
	f.read(t, ctx, 0)
	f.patch(t, ctx, f.roleID, true)
	f.read(t, ctx, 0)
	name := "Renamed delivery"
	_, err := f.ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: f.roleID, Name: &name})
	require.NoError(t, err)
	f.read(t, ctx, 0)

	// A real effective-access transition, unlike replay, adds the server again.
	f.patch(t, ctx, f.roleID, false)
	f.read(t, ctx, 0)
	f.patch(t, ctx, f.roleID, true)
	require.Equal(t, f.serverID.String(), *f.read(t, ctx, 1).Servers[0].McpServerID)
}

func TestService_UpdateRole_ProjectConnectGrantObeysConnectExclusion(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	projectID := f.ac.ProjectID.String()
	broad := &gen.RoleGrant{Scope: string(authz.ScopeMCPConnect), Selectors: []*gen.Selector{{ResourceKind: "mcp", ResourceID: "*", ProjectID: &projectID}}}
	blocked := &gen.RoleGrant{Scope: string(authz.ScopeMCPBlockedConnect), Selectors: []*gen.Selector{{ResourceKind: "mcp", ResourceID: f.toolsetID.String()}}}
	_, err := f.ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: f.roleID, AddGrants: []*gen.RoleGrant{broad}})
	require.NoError(t, err)
	require.Equal(t, f.serverID.String(), *f.read(t, ctx, 1).Servers[0].McpServerID)
	_, err = f.ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: f.roleID, AddGrants: []*gen.RoleGrant{blocked}})
	require.NoError(t, err)
	f.read(t, ctx, 0)
	_, err = f.ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: f.roleID, RemoveGrants: []*gen.RoleGrant{blocked}})
	require.NoError(t, err)
	f.read(t, ctx, 1)
	_, err = f.ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: f.roleID, RemoveGrants: []*gen.RoleGrant{broad}})
	require.NoError(t, err)
	f.read(t, ctx, 0)
}

func TestService_UpdateRole_ReadAndWriteGrantsDoNotDeliver(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	projectID := f.ac.ProjectID.String()
	for _, scope := range []authz.Scope{authz.ScopeMCPRead, authz.ScopeMCPWrite} {
		broad := &gen.RoleGrant{Scope: string(scope), Selectors: []*gen.Selector{{ResourceKind: "mcp", ResourceID: "*", ProjectID: &projectID}}}
		_, err := f.ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: f.roleID, AddGrants: []*gen.RoleGrant{broad}})
		require.NoError(t, err)
		f.read(t, ctx, 0)
	}
	f.patch(t, ctx, f.roleID, true)
	require.Equal(t, f.serverID.String(), *f.read(t, ctx, 1).Servers[0].McpServerID)
}

func TestService_UpdateRole_NewReadGrantDoesNotRestoreRemovedServer(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	f.patch(t, ctx, f.roleID, true)
	removedID := f.read(t, ctx, 1).Servers[0].ID
	require.NoError(t, f.pluginService.RemovePluginServer(ctx, &plugingen.RemovePluginServerPayload{ID: removedID, PluginID: f.plugin.ID}))
	projectID := f.ac.ProjectID.String()
	broad := &gen.RoleGrant{Scope: string(authz.ScopeMCPRead), Selectors: []*gen.Selector{{ResourceKind: "mcp", ResourceID: "*", ProjectID: &projectID}}}
	_, err := f.ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: f.roleID, AddGrants: []*gen.RoleGrant{broad}})
	require.NoError(t, err)
	f.read(t, ctx, 0)
}

func TestService_UpdateRole_GrantWithoutMatchingAudienceLeavesPluginsUnchanged(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	unmatchedID := seedRole(t, ctx, f.ti.conn, f.ac.ActiveOrganizationID, mockRole("role_unmatched", "Unmatched", "unmatched", ""))
	beforePlugins, err := f.pluginService.ListPlugins(ctx, &plugingen.ListPluginsPayload{})
	require.NoError(t, err)
	f.patch(t, ctx, unmatchedID, true)
	f.read(t, ctx, 0)
	afterPlugins, err := f.pluginService.ListPlugins(ctx, &plugingen.ListPluginsPayload{})
	require.NoError(t, err)
	require.Equal(t, beforePlugins, afterPlugins, "grant changes never create plugins without a matching audience")
	f.patch(t, ctx, unmatchedID, false)
	f.read(t, ctx, 0)
}

func TestService_UpdateRole_DistinctGrantRestoresExplicitlyRemovedPluginServer(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	f.patch(t, ctx, f.roleID, true)
	removedID := f.read(t, ctx, 1).Servers[0].ID
	require.NoError(t, f.pluginService.RemovePluginServer(ctx, &plugingen.RemovePluginServerPayload{ID: removedID, PluginID: f.plugin.ID}))
	f.patch(t, ctx, f.roleID, true)
	f.read(t, ctx, 0)
	projectID := f.ac.ProjectID.String()
	newConnectGrant := &gen.RoleGrant{Scope: string(authz.ScopeMCPConnect), Selectors: []*gen.Selector{{ResourceKind: "mcp", ResourceID: "*", ProjectID: &projectID}}}
	_, err := f.ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: f.roleID, AddGrants: []*gen.RoleGrant{newConnectGrant}})
	require.NoError(t, err)
	require.Equal(t, f.serverID.String(), *f.read(t, ctx, 1).Servers[0].McpServerID)
}

func TestService_UpdateRole_NewApplicableGrantRestoresRemovalDespiteBroadAccess(t *testing.T) {
	t.Parallel()
	ctx, f := newRoleDeliveryFixture(t)
	projectID := f.ac.ProjectID.String()
	broad := &gen.RoleGrant{Scope: string(authz.ScopeMCPConnect), Selectors: []*gen.Selector{{ResourceKind: "mcp", ResourceID: "*", ProjectID: &projectID}}}
	_, err := f.ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: f.roleID, AddGrants: []*gen.RoleGrant{broad}})
	require.NoError(t, err)
	removedID := f.read(t, ctx, 1).Servers[0].ID
	require.NoError(t, f.pluginService.RemovePluginServer(ctx, &plugingen.RemovePluginServerPayload{ID: removedID, PluginID: f.plugin.ID}))
	// Replaying broad access does not undo the administrator's content choice.
	_, err = f.ti.service.UpdateRole(ctx, &gen.UpdateRolePayload{ID: f.roleID, AddGrants: []*gen.RoleGrant{broad}})
	require.NoError(t, err)
	f.read(t, ctx, 0)
	// A new applicable grant delivers the server even though effective Use never changed.
	f.patch(t, ctx, f.roleID, true)
	require.Equal(t, f.serverID.String(), *f.read(t, ctx, 1).Servers[0].McpServerID)
}

type roleDeliveryFixture struct {
	ti            *testInstance
	ac            *contextvalues.AuthContext
	pluginService *plugins.Service
	plugin        *plugingen.Plugin
	github        *roleDeliveryGitHub
	githubConfig  *plugins.GitHubConfig
	roleID        string
	toolsetID     uuid.UUID
	serverID      uuid.UUID
}

func newRoleDeliveryFixture(t *testing.T) (context.Context, *roleDeliveryFixture) {
	t.Helper()
	ctx, ti := newTestAccessService(t)
	ac, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.roles.On("UpdateRole", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&workos.Role{}, nil).Maybe()
	roleID := seedRole(t, ctx, ti.conn, ac.ActiveOrganizationID, mockRole("role_delivery", "Delivery", "delivery", ""))
	toolsetID := uuid.New()
	_, err := testrepo.New(ti.conn).CreateToolsetFixture(ctx, testrepo.CreateToolsetFixtureParams{ID: toolsetID, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID, Name: "Delivery server", Slug: "delivery-server"})
	require.NoError(t, err)
	require.NoError(t, toolsetsrepo.New(ti.conn).SetToolsetMCPEnabledByID(ctx, toolsetsrepo.SetToolsetMCPEnabledByIDParams{ID: toolsetID, ProjectID: *ac.ProjectID, McpEnabled: true}))
	serverID, err := testrepo.New(ti.conn).CreateRemoteMCPServerFixture(ctx, testrepo.CreateRemoteMCPServerFixtureParams{ID: uuid.New(), ProjectID: *ac.ProjectID, ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, Visibility: "private"})
	require.NoError(t, err)
	_, err = endpointrepo.New(ti.conn).CreateMCPEndpoint(ctx, endpointrepo.CreateMCPEndpointParams{ProjectID: *ac.ProjectID, McpServerID: uuid.NullUUID{UUID: serverID, Valid: true}, Slug: "delivery-endpoint"})
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	tracer := testenv.NewTracerProvider(t)
	redis, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessions := testenv.NewTestManager(t, logger, tracer, ti.conn, redis, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracer))
	github := &roleDeliveryGitHub{}
	github.Test(t)
	t.Cleanup(func() { github.AssertExpectations(t) })
	githubConfig := &plugins.GitHubConfig{Client: github, Org: "test-org", InstallationID: 12345}
	pluginService := plugins.NewService(logger, tracer, ti.conn, sessions, cache.NewRedisCacheAdapter(redis), ti.service.authz, ti.service.audit, githubConfig, "test", "https://example.com", nil, nil)
	ti.service.roleMgr = NewRoleManager(logger, ti.conn, ti.roles, ti.service.audit, plugins.PublicationRequests{Enabled: true}, nil)
	plugin, err := pluginService.CreatePlugin(ctx, &plugingen.CreatePluginPayload{Name: "Delivery audience"})
	require.NoError(t, err)
	_, err = pluginService.SetPluginAssignments(ctx, &plugingen.SetPluginAssignmentsPayload{PluginID: plugin.ID, PrincipalUrns: []string{"role:organization:" + roleID}})
	require.NoError(t, err)

	return ctx, &roleDeliveryFixture{ti: ti, ac: ac, pluginService: pluginService, plugin: plugin, github: github, githubConfig: githubConfig, roleID: roleID, toolsetID: toolsetID, serverID: serverID}
}

func (f *roleDeliveryFixture) patch(t *testing.T, ctx context.Context, id string, add bool) {
	t.Helper()
	grant := &gen.RoleGrant{Scope: string(authz.ScopeMCPConnect), Selectors: []*gen.Selector{{ResourceKind: "mcp", ResourceID: f.toolsetID.String()}}}
	payload := &gen.UpdateRolePayload{ID: id}
	if add {
		payload.AddGrants = []*gen.RoleGrant{grant}
	} else {
		payload.RemoveGrants = []*gen.RoleGrant{grant}
	}
	_, err := f.ti.service.UpdateRole(ctx, payload)
	require.NoError(t, err)
}

func (f *roleDeliveryFixture) read(t *testing.T, ctx context.Context, count int) *plugingen.Plugin {
	t.Helper()
	got, err := f.pluginService.GetPlugin(ctx, &plugingen.GetPluginPayload{ID: f.plugin.ID})
	require.NoError(t, err)
	require.Len(t, got.Servers, count)
	return got
}

// roleDeliveryGitHub mocks only the external boundary; artifact generation stays real.
type roleDeliveryGitHub struct{ mock.Mock }

func (g *roleDeliveryGitHub) CreateRepo(ctx context.Context, installationID int64, owner, repo string, private bool) error {
	return g.Called(ctx, installationID, owner, repo, private).Error(0)
}
func (g *roleDeliveryGitHub) PushFiles(ctx context.Context, installationID int64, owner, repo, branch, message string, files map[string][]byte) (string, error) {
	args := g.Called(ctx, installationID, owner, repo, branch, message, files)
	return args.String(0), args.Error(1)
}
func (g *roleDeliveryGitHub) AddCollaborator(ctx context.Context, installationID int64, owner, repo, username, permission string) error {
	return g.Called(ctx, installationID, owner, repo, username, permission).Error(0)
}
func (g *roleDeliveryGitHub) HasDirectCollaborator(ctx context.Context, installationID int64, owner, repo string) (bool, error) {
	args := g.Called(ctx, installationID, owner, repo)
	return args.Bool(0), args.Error(1)
}
func (g *roleDeliveryGitHub) GetRepoFiles(ctx context.Context, installationID int64, owner, repo, branch string) (map[string][]byte, error) {
	args := g.Called(ctx, installationID, owner, repo, branch)
	getFiles, _ := args.Get(0).(func() map[string][]byte)
	return getFiles(), args.Error(1)
}
func (g *roleDeliveryGitHub) GetFileContent(ctx context.Context, installationID int64, owner, repo, branch, path string) ([]byte, error) {
	args := g.Called(ctx, installationID, owner, repo, branch, path)
	content, _ := args.Get(0).([]byte)
	return content, args.Error(1)
}
