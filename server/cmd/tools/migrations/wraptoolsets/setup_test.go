package wraptoolsets

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/wraptoolsets/repo"
	collectionsrepo "github.com/speakeasy-api/gram/server/internal/collections/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	mcpmetadatarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{
		Postgres:   true,
		Redis:      false,
		ClickHouse: false,
		Temporal:   false,
		Presidio:   false,
	})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

// tenant is a seeded org/project pair in a per-test Postgres clone, plus
// helpers to hang toolsets, domains, environments, metadata, and collection
// attachments off it.
type tenant struct {
	pool      *pgxpool.Pool
	orgID     string
	projectID uuid.UUID
}

func seedTenant(t *testing.T) *tenant {
	t.Helper()
	ctx := t.Context()

	pool, err := infra.CloneTestDatabase(t, "wraptoolsets")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	now := time.Now().UTC()
	require.NoError(t, testrepo.New(pool).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID:                 orgID,
		Name:               "wraptoolsets test org",
		Slug:               "wrap-" + uuid.NewString()[:8],
		GramAccountType:    "free",
		Whitelisted:        true,
		FreeTrialStartedAt: pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
		FreeTrialEndsAt:    pgtype.Timestamptz{Time: now.AddDate(0, 0, 14), Valid: true, InfinityModifier: pgtype.Finite},
	}))

	project, err := projectsrepo.New(pool).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "wrap",
		Slug:           "wrap",
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	return &tenant{
		pool:      pool,
		orgID:     orgID,
		projectID: project.ID,
	}
}

func (tn *tenant) queries() *repo.Queries {
	return repo.New(tn.pool)
}

// candidateSpec describes the publishing state of a seeded toolset. A
// non-empty mcpSlug makes it a wrap candidate.
type candidateSpec struct {
	mcpSlug        string
	mcpEnabled     bool
	mcpIsPublic    bool
	defaultEnvSlug string
	customDomainID uuid.NullUUID
}

func (tn *tenant) newToolset(t *testing.T, spec candidateSpec) repo.Toolset {
	t.Helper()
	ctx := t.Context()

	slug := "ts-" + uuid.NewString()[:8]
	toolset, err := repo.New(tn.pool).InsertPreSwapToolsetFixture(ctx, repo.InsertPreSwapToolsetFixtureParams{
		OrganizationID:         tn.orgID,
		ProjectID:              tn.projectID,
		Name:                   "toolset " + slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: conv.ToPGTextEmpty(spec.defaultEnvSlug),
		McpSlug:                conv.ToPGTextEmpty(spec.mcpSlug),
		McpEnabled:             spec.mcpEnabled,
		McpIsPublic:            spec.mcpIsPublic,
		CustomDomainID:         spec.customDomainID,
	})
	require.NoError(t, err)

	return toolset
}

func (tn *tenant) newCustomDomain(t *testing.T) uuid.UUID {
	t.Helper()

	domain, err := customdomainsrepo.New(tn.pool).CreateCustomDomain(t.Context(), customdomainsrepo.CreateCustomDomainParams{
		OrganizationID:  tn.orgID,
		Domain:          "mcp-" + uuid.NewString()[:8] + ".example.test",
		IngressName:     pgtype.Text{String: "", Valid: false},
		CertSecretName:  pgtype.Text{String: "", Valid: false},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)

	return domain.ID
}

// softDeleteCustomDomain tombstones the tenant's live domain (there is at
// most one per organization).
func (tn *tenant) softDeleteCustomDomain(t *testing.T) {
	t.Helper()
	require.NoError(t, customdomainsrepo.New(tn.pool).DeleteCustomDomain(t.Context(), tn.orgID))
}

func (tn *tenant) newEnvironment(t *testing.T, slug string) uuid.UUID {
	t.Helper()

	env, err := environmentsrepo.New(tn.pool).CreateEnvironment(t.Context(), environmentsrepo.CreateEnvironmentParams{
		OrganizationID: tn.orgID,
		ProjectID:      tn.projectID,
		Name:           "env " + slug,
		Slug:           slug,
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	return env.ID
}

func (tn *tenant) attachMetadata(t *testing.T, toolsetID uuid.UUID) mcpmetadatarepo.McpMetadatum {
	t.Helper()

	metadata, err := mcpmetadatarepo.New(tn.pool).UpsertMetadata(t.Context(), mcpmetadatarepo.UpsertMetadataParams{
		ToolsetID:                 uuid.NullUUID{UUID: toolsetID, Valid: true},
		ProjectID:                 tn.projectID,
		ExternalDocumentationUrl:  conv.ToPGText("https://docs.example.test"),
		ExternalDocumentationText: pgtype.Text{String: "", Valid: false},
		LogoID:                    uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Instructions:              pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		InstallationOverrideUrl:   pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	return metadata
}

func (tn *tenant) newCollection(t *testing.T) uuid.UUID {
	t.Helper()

	collection, err := collectionsrepo.New(tn.pool).CreateOrganizationMcpCollection(t.Context(), collectionsrepo.CreateOrganizationMcpCollectionParams{
		OrganizationID: tn.orgID,
		Name:           "collection",
		Description:    pgtype.Text{String: "", Valid: false},
		Slug:           "col-" + uuid.NewString()[:8],
		Visibility:     "private",
	})
	require.NoError(t, err)

	return collection.ID
}

func (tn *tenant) attachToolsetToCollection(t *testing.T, collectionID, toolsetID uuid.UUID) collectionsrepo.OrganizationMcpCollectionServerAttachment {
	t.Helper()

	attachment, err := collectionsrepo.New(tn.pool).AttachServerToOrganizationMcpCollection(t.Context(), collectionsrepo.AttachServerToOrganizationMcpCollectionParams{
		ToolsetID:      uuid.NullUUID{UUID: toolsetID, Valid: true},
		PublishedBy:    conv.ToPGText("user_test_fixture"),
		CollectionID:   collectionID,
		OrganizationID: tn.orgID,
	})
	require.NoError(t, err)

	return attachment
}

func (tn *tenant) detachToolsetFromCollection(t *testing.T, collectionID, toolsetID uuid.UUID) {
	t.Helper()

	require.NoError(t, collectionsrepo.New(tn.pool).DetachServerFromOrganizationMcpCollection(t.Context(), collectionsrepo.DetachServerFromOrganizationMcpCollectionParams{
		ToolsetID:      uuid.NullUUID{UUID: toolsetID, Valid: true},
		CollectionID:   collectionID,
		OrganizationID: tn.orgID,
	}))
}

func (tn *tenant) newPlugin(t *testing.T) uuid.UUID {
	t.Helper()

	slug := "plg-" + uuid.NewString()[:8]
	pluginID, err := tn.queries().InsertPluginFixture(t.Context(), repo.InsertPluginFixtureParams{
		OrganizationID: tn.orgID,
		ProjectID:      tn.projectID,
		Name:           "plugin " + slug,
		Slug:           slug,
	})
	require.NoError(t, err)

	return pluginID
}

// pluginServerSpec describes a plugin_servers fixture row. Exactly one of
// toolsetID / mcpServerID must be valid; a non-zero deletedAt seeds
// soft-deleted history.
type pluginServerSpec struct {
	pluginID    uuid.UUID
	toolsetID   uuid.NullUUID
	mcpServerID uuid.NullUUID
	displayName string
	deletedAt   time.Time
}

func (tn *tenant) newPluginServer(t *testing.T, spec pluginServerSpec) repo.PluginServer {
	t.Helper()

	deletedAt := pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite}
	if !spec.deletedAt.IsZero() {
		deletedAt = pgtype.Timestamptz{Time: spec.deletedAt, Valid: true, InfinityModifier: pgtype.Finite}
	}
	row, err := tn.queries().InsertPluginServerFixture(t.Context(), repo.InsertPluginServerFixtureParams{
		PluginID:    spec.pluginID,
		ToolsetID:   spec.toolsetID,
		McpServerID: spec.mcpServerID,
		DisplayName: spec.displayName,
		Policy:      "required",
		SortOrder:   0,
		DeletedAt:   deletedAt,
	})
	require.NoError(t, err)

	return row
}

func runWrap(t *testing.T, tn *tenant, opts Options) *Report {
	t.Helper()

	report, err := Run(t.Context(), tn.pool, opts)
	require.NoError(t, err)
	require.NotNil(t, report)
	return report
}

// wrapToolset applies the default wrap mode for the tenant and returns the
// wrapper mcp_server id created for the toolset.
func wrapToolset(t *testing.T, tn *tenant, toolsetID uuid.UUID) uuid.UUID {
	t.Helper()

	report := runWrap(t, tn, applyOptions())
	for _, row := range report.Rows {
		if row.ToolsetID == toolsetID {
			require.Equal(t, OutcomeCreated, row.Outcome)
			require.NotNil(t, row.McpServerID)
			return *row.McpServerID
		}
	}
	t.Fatalf("toolset %s was not wrapped", toolsetID)
	return uuid.Nil
}

func applyOptions() Options {
	return Options{
		DryRun:          false,
		After:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Limit:           0,
		ProjectID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ClearDeadDomain: false,
		MoveDependents:  true,
		MovePlugins:     false,
	}
}

func dryRunOptions() Options {
	opts := applyOptions()
	opts.DryRun = true
	return opts
}

func movePluginsApplyOptions() Options {
	opts := applyOptions()
	opts.MovePlugins = true
	return opts
}

func movePluginsDryRunOptions() Options {
	opts := movePluginsApplyOptions()
	opts.DryRun = true
	return opts
}
