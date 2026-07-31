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
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
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

func (tn *tenant) newToolset(t *testing.T, spec candidateSpec) toolsetsrepo.Toolset {
	t.Helper()
	ctx := t.Context()

	slug := "ts-" + uuid.NewString()[:8]
	toolsets := toolsetsrepo.New(tn.pool)
	toolset, err := toolsets.CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         tn.orgID,
		ProjectID:              tn.projectID,
		Name:                   "toolset " + slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: conv.ToPGTextEmpty(spec.defaultEnvSlug),
		McpSlug:                conv.ToPGTextEmpty(spec.mcpSlug),
		McpEnabled:             spec.mcpEnabled,
	})
	require.NoError(t, err)

	if spec.mcpIsPublic {
		require.NoError(t, toolsets.SetToolsetMCPPublicByID(ctx, toolsetsrepo.SetToolsetMCPPublicByIDParams{
			McpIsPublic: true,
			ID:          toolset.ID,
			ProjectID:   tn.projectID,
		}))
	}
	if spec.customDomainID.Valid {
		require.NoError(t, toolsets.SetToolsetCustomDomain(ctx, toolsetsrepo.SetToolsetCustomDomainParams{
			CustomDomainID: spec.customDomainID,
			Slug:           toolset.Slug,
			ProjectID:      tn.projectID,
		}))
	}

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

func runWrap(t *testing.T, tn *tenant, opts Options) *Report {
	t.Helper()

	report, err := Run(t.Context(), tn.pool, opts)
	require.NoError(t, err)
	require.NotNil(t, report)
	return report
}

func applyOptions() Options {
	return Options{
		DryRun:          false,
		After:           uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Limit:           0,
		ProjectID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ClearDeadDomain: false,
	}
}

func dryRunOptions() Options {
	opts := applyOptions()
	opts.DryRun = true
	return opts
}
