//nolint:wrapcheck // Integration assertions intentionally return test setup errors directly.
package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	skillsservice "github.com/speakeasy-api/gram/server/internal/skills"
	skillsrepo "github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// The skills lane is only as good as its slowest-moving joint: a Platform MCP
// tool call has to travel the OAuth endpoint, the tool schema, the acting
// principal, the skills management service, RBAC, and Postgres, and come back
// as something a model can act on. The unit tests hold each joint still; this
// one drives the whole run with a real MCP client, a real skills service, and a
// real database.
func TestPlatformMCPSkillsToolsAuthorAndDistributeEndToEnd(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newSkillsVerticalFixture(t, ctx, "platform_mcp_skills_vertical", skillsVerticalOptions{capabilityEnabled: true, grantAdmin: true})
	session := fixture.session

	// Authoring. The result has to say the skill is inert, because a model that
	// reads "created" and stops leaves a skill nothing will ever load.
	created := callSkillsTool[SkillAuthoringResult](t, ctx, session, "create_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"content":      skillsFixtureManifest("catalog-add", "Adds a reviewed MCP from the catalogue.", "Ask which project first."),
	})
	require.True(t, created.CreatedSkill)
	require.True(t, created.CreatedVersion)
	require.False(t, created.Distributed)
	require.Contains(t, created.InertMessage, "no agent loads it yet")
	require.Equal(t, "distribute_skill", created.NextAction)
	require.Contains(t, skillTargetNames(created.DistributionTargets), "Marketing")

	stored, err := skillsrepo.New(fixture.conn).GetSkillState(ctx, skillsrepo.GetSkillStateParams{
		ProjectID: fixture.project.ID,
		SkillID:   uuid.MustParse(created.Skill.ID),
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, stored.VersionCount)
	require.Equal(t, created.Version.ID, stored.LatestVersionID.String())

	// Reads withhold manifest content unless the caller asks for it.
	read := callSkillsTool[GetSkillOutput](t, ctx, session, "get_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
	})
	require.NotNil(t, read.LatestVersion)
	require.Empty(t, read.LatestVersion.Content)

	withContent := callSkillsTool[GetSkillOutput](t, ctx, session, "get_skill", map[string]any{
		"project_slug":    fixture.project.Slug,
		"skill_id":        created.Skill.ID,
		"include_content": true,
	})
	require.Contains(t, withContent.LatestVersion.Content, "Ask which project first.")

	// A correction is a new immutable version, guarded by the version the
	// caller read.
	revised := callSkillsTool[SkillAuthoringResult](t, ctx, session, "add_skill_version", map[string]any{
		"project_slug":               fixture.project.Slug,
		"skill_id":                   created.Skill.ID,
		"content":                    skillsFixtureManifest("catalog-add", "Adds a reviewed MCP from the catalogue.", "Ask which project first, then which catalogue entry."),
		"expected_latest_version_id": created.Version.ID,
	})
	require.True(t, revised.CreatedVersion)
	require.NotEqual(t, created.Version.ID, revised.Version.ID)

	// The same token again is stale, and a stale write is refused rather than
	// applied on top of the version it did not see.
	conflict := callSkillsRefusal(t, ctx, session, "add_skill_version", map[string]any{
		"project_slug":               fixture.project.Slug,
		"skill_id":                   created.Skill.ID,
		"content":                    skillsFixtureManifest("catalog-add", "Adds a reviewed MCP from the catalogue.", "A third opinion."),
		"expected_latest_version_id": created.Version.ID,
	})
	require.Equal(t, "conflict", conflict.Code)

	versions, err := skillsrepo.New(fixture.conn).GetSkillState(ctx, skillsrepo.GetSkillStateParams{
		ProjectID: fixture.project.ID,
		SkillID:   uuid.MustParse(created.Skill.ID),
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, versions.VersionCount, "the refused write recorded nothing")

	// Naming a target that does not exist distributes nothing — least of all to
	// the default plugin, which is exactly the plugin an implicit fallback would
	// have picked here.
	missing := callSkillsRefusal(t, ctx, session, "distribute_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
		"plugin":       "marketng",
	})
	require.Equal(t, "not_found", missing.Code)

	require.Empty(t, listSkillDistributions(t, ctx, fixture.conn, fixture.project.ID, created.Skill.ID))

	// Distribution is the activation step, and it echoes back the target the
	// caller's name resolved to.
	distributed := callSkillsTool[DistributeSkillOutput](t, ctx, session, "distribute_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
		"plugin":       "marketing",
	})
	require.Equal(t, SkillTargetPlugin, distributed.Target.Kind)
	require.Equal(t, fixture.marketingPluginID.String(), distributed.Target.ID)
	require.Equal(t, revised.Version.ID, distributed.ResolvedVersionID, "a distribution that pins nothing tracks the latest valid version")

	// Idempotent on project, target, and skill: a repeat converges rather than
	// attaching the skill twice.
	repeat := callSkillsTool[DistributeSkillOutput](t, ctx, session, "distribute_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
		"plugin":       "marketing",
	})
	require.Equal(t, distributed.DistributionID, repeat.DistributionID)
	require.Len(t, listSkillDistributions(t, ctx, fixture.conn, fixture.project.ID, created.Skill.ID), 1)
}

// A skill lives in one project, and naming another project's plugin must not
// reach it even when the same connection is authorized for both.
func TestPlatformMCPDistributeSkillRefusesATargetInAnotherProject(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newSkillsVerticalFixture(t, ctx, "platform_mcp_skills_cross_project", skillsVerticalOptions{capabilityEnabled: true, grantAdmin: true})

	otherSlug := "other-" + uuid.NewString()[:8]
	otherProject, err := projectsrepo.New(fixture.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           otherSlug,
		Slug:           otherSlug,
		OrganizationID: fixture.principal.OrganizationID,
	})
	require.NoError(t, err)
	_, err = pluginsrepo.New(fixture.conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: fixture.principal.OrganizationID,
		ProjectID:      otherProject.ID,
		Name:           "Elsewhere",
		Slug:           "elsewhere",
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	created := callSkillsTool[SkillAuthoringResult](t, ctx, fixture.session, "create_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"content":      skillsFixtureManifest("scoped", "Scoped to one project.", "Body."),
	})

	refusal := callSkillsRefusal(t, ctx, fixture.session, "distribute_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"skill_id":     created.Skill.ID,
		"plugin":       "elsewhere",
	})

	require.Equal(t, "not_found", refusal.Code)
}

// A capability that is off must still answer. The endpoint keeps serving, the
// tool keeps existing, and the caller gets a reason rather than a tool that
// silently disappeared from the manifest.
func TestPlatformMCPSkillsToolsRefuseReadablyWhenTheCapabilityIsOff(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newSkillsVerticalFixture(t, ctx, "platform_mcp_skills_capability_off", skillsVerticalOptions{capabilityEnabled: false, grantAdmin: true})

	tools, err := fixture.session.ListTools(ctx, nil)
	require.NoError(t, err)
	require.Contains(t, toolNames(tools.Tools), "create_skill")

	refusal := callSkillsRefusal(t, ctx, fixture.session, "create_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"content":      skillsFixtureManifest("gated", "Gated by the rollout.", "Body."),
	})

	require.Equal(t, unavailableCode, refusal.Code)
}

// Authorization is the acting user's, not the surface's. A connection whose
// user holds no role in an RBAC-enabled organization reaches Postgres and is
// refused there, rather than being trusted because it authenticated.
func TestPlatformMCPSkillsToolsRefuseAUserWithoutGrants(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	fixture := newSkillsVerticalFixture(t, ctx, "platform_mcp_skills_ungranted", skillsVerticalOptions{capabilityEnabled: true, grantAdmin: false})

	refusal := callSkillsRefusal(t, ctx, fixture.session, "create_skill", map[string]any{
		"project_slug": fixture.project.Slug,
		"content":      skillsFixtureManifest("ungranted", "Written without grants.", "Body."),
	})

	require.Equal(t, "forbidden", refusal.Code)

	skills, err := skillsrepo.New(fixture.conn).ListSkills(ctx, skillsrepo.ListSkillsParams{ProjectID: fixture.project.ID, PageLimit: 10})
	require.NoError(t, err)
	require.Empty(t, skills)
}

type skillsVerticalFixture struct {
	conn              *pgxpool.Pool
	principal         Principal
	project           ResolvedProject
	session           *mcp.ClientSession
	marketingPluginID uuid.UUID
}

// newSkillsVerticalFixture composes the production wiring against a real
// database: the real skills management service, the Postgres target inventory,
// the real registration store as project resolver, and the runtime's own HTTP
// handler behind an MCP client. Only the OAuth token exchange is stood in for,
// because minting one would test the OAuth lane rather than this one.
type skillsVerticalOptions struct {
	// capabilityEnabled is the Platform MCP kill switch as the skills tools see
	// it. The runtime's own gate stays on so the endpoint keeps serving; this
	// isolates what a caller gets from a tool whose capability is off.
	capabilityEnabled bool
	// grantAdmin gives the acting user real organization-admin grants. Off, the
	// call travels the whole path and is refused by RBAC at the end of it.
	grantAdmin bool
}

func newSkillsVerticalFixture(t *testing.T, ctx context.Context, name string, options skillsVerticalOptions) *skillsVerticalFixture {
	t.Helper()

	conn, err := platformMCPInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)

	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	// The endpoint refuses a principal missing any half of its OAuth identity,
	// so the fixture presents the complete one a real connection carries.
	principal.ClientID = "client-" + uuid.NewString()
	principal.Surface = SurfacePlatformMCP

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	features := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)
	siteURL, err := url.Parse("https://app.getgram.test")
	require.NoError(t, err)
	skills := skillsservice.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, features, audit.NewLogger(), nil, siteURL)

	_, err = featurerepo.New(conn).EnableFeature(ctx, featurerepo.EnableFeatureParams{
		OrganizationID: principal.OrganizationID,
		FeatureName:    string(productfeatures.FeatureSkills),
	})
	require.NoError(t, err)
	features.UpdateFeatureCache(ctx, principal.OrganizationID, productfeatures.FeatureSkills, true)

	// The acting user holds real grants rather than context overrides, so the
	// test exercises the authorization the OAuth surface actually relies on.
	require.NoError(t, testrepo.New(conn).InsertUserFixture(ctx, testrepo.InsertUserFixtureParams{
		ID:          principal.UserID,
		Email:       principal.UserID + "@example.test",
		DisplayName: "Platform MCP skills test user",
	}))
	require.NoError(t, testrepo.New(conn).CreateOrganizationUserRelationshipFixture(ctx, testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: principal.OrganizationID,
		UserID:         pgtype.Text{String: principal.UserID, Valid: true},
	}))
	if options.grantAdmin {
		require.NoError(t, authz.NewProvisioner(conn).ProvisionOrganizationAdmin(ctx, principal.OrganizationID, authz.InitialOrganizationAdmin{
			UserID:             principal.UserID,
			WorkOSUserID:       principal.UserID,
			WorkOSMembershipID: "membership-" + uuid.NewString(),
		}))
	} else {
		// A member of the organization holding no role. Seeding the roles without
		// assigning one is what makes this "authenticated but unauthorized"
		// rather than an organization that predates RBAC.
		require.NoError(t, authz.SeedSystemRoleGrants(ctx, conn, principal.OrganizationID))
	}

	plugins := pluginsrepo.New(conn)
	_, err = plugins.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	marketing, err := plugins.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
		Name:           "Marketing",
		Slug:           "marketing",
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	_, err = assistantsrepo.New(conn).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID:       project.ID,
		OrganizationID:  principal.OrganizationID,
		CreatedByUserID: pgtype.Text{String: principal.UserID, Valid: true},
		Name:            "Support",
		Model:           "claude-sonnet-5",
		Instructions:    "Help customers.",
		WarmTtlSeconds:  300,
		MaxConcurrency:  1,
		Status:          "active",
	})
	require.NoError(t, err)

	store, err := NewRegistrationStore(conn, RegistrationStoreConfig{ActiveRegistrationCap: 5})
	require.NoError(t, err)
	allow := func() Limiter { return &recordingOperationLimiter{result: ratelimit.Result{Allowed: true}} }
	skillsSurface := NewSkillsService(
		skills,
		NewPostgresSkillTargets(conn),
		store,
		authzEngine,
		NewCatalogRegistrationGate(testGate{enabled: options.capabilityEnabled}),
		OperationBudget{Connection: allow(), Organization: allow()},
	)

	runtime := NewRuntimeWithLifecycle(
		logger, &testAuthenticator{principal: principal}, testGate{enabled: true}, &testAuthorizer{},
		"", "test-cursor-key", nil, nil, nil, nil, nil, nil, nil, nil, skillsSurface, nil, nil, CatalogDescriptor{},
	)
	server := httptest.NewServer(runtime.Handler())
	t.Cleanup(server.Close)

	// The endpoint authenticates every request, so the client presents a bearer
	// token on each one exactly as a real MCP client would.
	httpClient := server.Client()
	httpClient.Transport = bearerTokenTransport{base: httpClient.Transport}
	transport := &mcp.StreamableClientTransport{
		Endpoint:   server.URL,
		HTTPClient: httpClient,
		MaxRetries: 0,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "skills-vertical-test", Version: "0.0.1"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	return &skillsVerticalFixture{
		conn:              conn,
		principal:         principal,
		project:           project,
		session:           session,
		marketingPluginID: marketing.ID,
	}
}

// callSkillsTool calls one tool and decodes its structured result, failing the
// test if the call came back as a refusal.
func callSkillsTool[Out any](t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any) Out {
	t.Helper()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	require.NoError(t, err)
	require.Falsef(t, result.IsError, "tool %q refused: %s", name, skillsToolText(t, result))

	var out Out
	require.NoError(t, json.Unmarshal([]byte(skillsToolText(t, result)), &out))
	return out
}

// callSkillsRefusal calls one tool that is expected to refuse, and returns the
// structured refusal. A refusal must arrive as a readable result rather than a
// transport error, or the model loses the reason.
func callSkillsRefusal(t *testing.T, ctx context.Context, session *mcp.ClientSession, name string, arguments map[string]any) skillsRefusalResult {
	t.Helper()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	require.NoError(t, err)
	require.Truef(t, result.IsError, "tool %q was expected to refuse", name)

	var refusal skillsRefusalResult
	require.NoError(t, json.Unmarshal([]byte(skillsToolText(t, result)), &refusal))
	require.NotEmpty(t, refusal.Message)
	return refusal
}

func skillsToolText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()

	require.NotEmpty(t, result.Content)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	return text.Text
}

func skillTargetNames(targets []SkillTarget) []string {
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.Name)
	}
	return names
}

func skillsFixtureManifest(name, description, body string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", name, description, body)
}

type bearerTokenTransport struct{ base http.RoundTripper }

func (t bearerTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Header.Set("Authorization", "Bearer platform-mcp-test-token")
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(cloned)
}

func listSkillDistributions(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID, skillID string) []skillsrepo.ListActiveSkillDistributionsRow {
	t.Helper()

	rows, err := skillsrepo.New(conn).ListActiveSkillDistributions(ctx, skillsrepo.ListActiveSkillDistributionsParams{
		ProjectID:       projectID,
		SkillID:         uuid.NullUUID{UUID: uuid.MustParse(skillID), Valid: true},
		PluginID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		CursorCreatedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		CursorID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		PageLimit:       50,
	})
	require.NoError(t, err)
	return rows
}

func toolNames(tools []*mcp.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}
