package gram

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestPlatformMCPPublicationEvidenceRefusesPublisherAsReader(t *testing.T) {
	t.Parallel()
	logger := testenv.NewLogger(t)
	publisher := plugins.NewPublisher(logger, nil, nil, nil, "test", "https://api.example.test", nil)
	inventory := platformmcp.NewPluginsService(nil, platformmcp.OperationBudget{}, "test-key")
	for _, management := range []*plugins.Service{nil, publisher} {
		err := withPlatformMCPPublicationEvidence(inventory, platformMCPConfig{PluginPublisher: publisher, PluginManagement: management})
		require.ErrorIs(t, err, plugins.ErrPublicationEvidenceUnavailable)
	}
	require.NoError(t, withPlatformMCPPublicationEvidence(inventory, platformMCPConfig{}))
}

func TestPlatformMCPGetPluginUsesAuthorizedPublicationEvidence(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	infra, cleanup, err := testenv.Launch(ctx, testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	pool, err := infra.CloneTestDatabase(t, "platform_mcp_publication_evidence")
	require.NoError(t, err)
	organizationID := "org_" + uuid.NewString()
	_, err = organizationsrepo.New(pool).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{ID: organizationID, Name: "Evidence test", Slug: "evidence-" + uuid.NewString()})
	require.NoError(t, err)
	project, err := projectsrepo.New(pool).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Evidence test", Slug: "evidence", OrganizationID: organizationID})
	require.NoError(t, err)
	plugin, err := pluginsrepo.New(pool).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{OrganizationID: organizationID, ProjectID: project.ID, Name: "Paused", Slug: "paused", AutoSyncEnabled: pgtype.Bool{Bool: false, Valid: true}})
	require.NoError(t, err)
	_, err = pluginsrepo.New(pool).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{ProjectID: project.ID, InstallationID: 1, RepoOwner: "fixture", RepoName: "fixture"})
	require.NoError(t, err)
	ctx = contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{ActiveOrganizationID: organizationID, ProjectID: &project.ID})
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{{PrincipalUrn: "role:admin", Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, organizationID)}})
	logger := testenv.NewLogger(t)
	engine := authz.NewEngine(logger, pool, nil, nil)
	management := plugins.NewService(logger, testenv.NewTracerProvider(t), pool, &sessions.Manager{}, nil, engine, nil, nil, "test", "https://api.example.test", nil, nil)
	publisher := plugins.NewPublisher(logger, pool, nil, nil, "test", "https://api.example.test", nil)
	limiter := allowingPlatformMCPBudget{}
	inventory := platformmcp.NewPluginsService(pool, platformmcp.OperationBudget{Connection: limiter, Organization: limiter}, "test-key")
	require.NoError(t, withPlatformMCPPublicationEvidence(inventory, platformMCPConfig{PluginPublisher: publisher, PluginManagement: management}))
	output, err := inventory.GetPlugin(ctx, platformmcp.Principal{OrganizationID: organizationID, ConnectionID: uuid.NewString(), Generation: uuid.NewString()}, platformmcp.GetPluginInput{ProjectID: project.ID.String(), Plugin: plugin.Slug})
	require.NoError(t, err)
	require.NotNil(t, output.PublicationEvidence)
	require.False(t, output.PublicationEvidence.Unavailable)
	require.False(t, output.PublicationEvidence.NotConfigured)
	require.Equal(t, new(false), output.PublicationEvidence.Fresh)
	require.True(t, output.PublicationEvidence.AwaitingManualSync)
}
