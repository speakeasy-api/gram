package mcpskills_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: false, ClickHouse: false, Temporal: false, Presidio: false})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

// fixture is one organization with one project and one hosted toolset, the
// endpoint every resolution in a test is scoped to.
type fixture struct {
	conn           *pgxpool.Pool
	organizationID string
	projectID      uuid.UUID
	toolsetID      uuid.UUID
	resolver       *mcpskills.Resolver
}

const (
	testUserID    = "user_mcpskills"
	testUserEmail = "alice@example.test"
)

func newFixture(t *testing.T) (context.Context, *fixture) {
	t.Helper()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	organizationID := "org-mcpskills-" + uuid.NewString()[:8]
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: organizationID, Slug: organizationID, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	f := &fixture{
		conn:           conn,
		organizationID: organizationID,
		projectID:      uuid.Nil,
		toolsetID:      uuid.Nil,
		resolver:       mcpskills.NewResolver(testenv.NewLogger(t), conn),
	}
	f.projectID = createProject(t, ctx, f, "mcpskills-"+uuid.NewString()[:8])
	f.toolsetID = createToolset(t, ctx, f, f.projectID, "served-toolset")
	return ctx, f
}

func createProject(t *testing.T, ctx context.Context, f *fixture, slug string) uuid.UUID {
	t.Helper()

	project, err := projectsrepo.New(f.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: slug, Slug: slug, OrganizationID: f.organizationID,
	})
	require.NoError(t, err)
	return project.ID
}

func createToolset(t *testing.T, ctx context.Context, f *fixture, projectID uuid.UUID, slug string) uuid.UUID {
	t.Helper()

	toolset, err := toolsetsrepo.New(f.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: f.organizationID, ProjectID: projectID, Name: slug, Slug: slug,
		Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{}, McpSlug: conv.ToPGText(slug), McpEnabled: true,
	})
	require.NoError(t, err)

	// The canonical mcp_servers row shares the toolset's id, and plugins
	// contain the toolset through it.
	_, err = mcpserversrepo.New(f.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: projectID, Name: conv.ToPGText(slug), Slug: conv.ToPGText(slug),
		ToolsetID: conv.ToNullUUID(toolset.ID), Visibility: visibility.Private,
	})
	require.NoError(t, err)
	return toolset.ID
}

// createFrontingServer gives the fixture toolset a second, non-canonical
// mcp_servers row, which a caller passes as the server fronting the toolset.
func createFrontingServer(t *testing.T, ctx context.Context, f *fixture) uuid.UUID {
	t.Helper()

	server, err := mcpserversrepo.New(f.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: f.projectID, Name: conv.ToPGText("served"), Slug: conv.ToPGText("served"),
		ToolsetID: conv.ToNullUUID(f.toolsetID), Visibility: visibility.Private,
	})
	require.NoError(t, err)
	return server.ID
}

func skillContent(name, description string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# %s\n", name, description, name)
}

func rawSHA256(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

func createSkill(t *testing.T, ctx context.Context, f *fixture, projectID uuid.UUID, name string) uuid.UUID {
	t.Helper()

	skill, err := skillsrepo.New(f.conn).CreateSkill(ctx, skillsrepo.CreateSkillParams{
		ProjectID: projectID, Name: name, DisplayName: name, Summary: pgtype.Text{},
	})
	require.NoError(t, err)
	return skill.ID
}

// addVersion records one spec-valid version of a skill. Versions added in
// sequence are strictly newer, which is the order the resolver relies on.
func addVersion(t *testing.T, ctx context.Context, f *fixture, projectID, skillID uuid.UUID, content string) uuid.UUID {
	t.Helper()

	version, err := skillsrepo.New(f.conn).CreateSkillVersion(ctx, skillsrepo.CreateSkillVersionParams{
		Content: content, CanonicalSha256: rawSHA256(content), RawSha256: rawSHA256(content), Description: pgtype.Text{},
		Metadata: []byte("{}"), SpecValid: true, ValidationErrors: []byte("[]"), CreatedByUserID: testUserID,
		ProjectID: projectID, SkillID: skillID,
	})
	require.NoError(t, err)
	return version.ID
}

func createPlugin(t *testing.T, ctx context.Context, f *fixture, projectID uuid.UUID, slug string) uuid.UUID {
	t.Helper()

	plugin, err := pluginsrepo.New(f.conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: f.organizationID, ProjectID: projectID, Name: slug, Slug: slug, Description: pgtype.Text{},
	})
	require.NoError(t, err)
	return plugin.ID
}

func containToolset(t *testing.T, ctx context.Context, f *fixture, pluginID, toolsetID uuid.UUID) uuid.UUID {
	t.Helper()

	member, err := pluginsrepo.New(f.conn).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID: pluginID, McpServerID: conv.ToNullUUID(toolsetID),
		DisplayName: "toolset", Policy: "required", SortOrder: 0,
	})
	require.NoError(t, err)
	return member.ID
}

func containMCPServer(t *testing.T, ctx context.Context, f *fixture, pluginID, serverID uuid.UUID) {
	t.Helper()

	_, err := pluginsrepo.New(f.conn).AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{
		PluginID: pluginID, McpServerID: conv.ToNullUUID(serverID),
		DisplayName: "server", Policy: "required", SortOrder: 0,
	})
	require.NoError(t, err)
}

func assign(t *testing.T, ctx context.Context, f *fixture, pluginID uuid.UUID, principalURN string) {
	t.Helper()

	_, err := pluginsrepo.New(f.conn).AddPluginAssignment(ctx, pluginsrepo.AddPluginAssignmentParams{
		OrganizationID: f.organizationID, PrincipalUrn: principalURN, InstallMode: "default", PluginID: pluginID,
	})
	require.NoError(t, err)
}

func distribute(t *testing.T, ctx context.Context, f *fixture, projectID, skillID, pluginID uuid.UUID, pinnedVersionID uuid.NullUUID) {
	t.Helper()

	_, err := skillsrepo.New(f.conn).CreateSkillDistribution(ctx, skillsrepo.CreateSkillDistributionParams{
		ProjectID: projectID, SkillID: skillID, PluginID: conv.ToNullUUID(pluginID),
		AssistantID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, PinnedVersionID: pinnedVersionID,
		Channel: "plugin", CreatedByUserID: testUserID,
	})
	require.NoError(t, err)
}

// servedSkill stands up the common case: one skill with one version,
// distributed through a plugin that contains the fixture toolset and is
// assigned to principalURN.
func servedSkill(t *testing.T, ctx context.Context, f *fixture, name, principalURN string) (skillID, versionID, pluginID uuid.UUID) {
	t.Helper()

	skillID = createSkill(t, ctx, f, f.projectID, name)
	versionID = addVersion(t, ctx, f, f.projectID, skillID, skillContent(name, "Serves "+name+"."))
	pluginID = createPlugin(t, ctx, f, f.projectID, name+"-plugin")
	containToolset(t, ctx, f, pluginID, f.toolsetID)
	assign(t, ctx, f, pluginID, principalURN)
	distribute(t, ctx, f, f.projectID, skillID, pluginID, uuid.NullUUID{UUID: uuid.Nil, Valid: false})
	return skillID, versionID, pluginID
}

func emailPrincipal(email string) string {
	return urn.NewPrincipal(urn.PrincipalTypeEmail, email).String()
}

func (f *fixture) scope(principals ...string) mcpskills.Scope {
	return mcpskills.Scope{ProjectID: f.projectID, ToolsetID: f.toolsetID, MCPServerID: nil, PrincipalURNs: principals}
}
