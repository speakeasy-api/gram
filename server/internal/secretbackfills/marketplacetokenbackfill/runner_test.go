package marketplacetokenbackfill_test

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

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/plugins/marketplacetokens"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/secretbackfills/marketplacetokenbackfill"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var testInfra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	testInfra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type fixture struct {
	db      *pgxpool.Pool
	orgID   string
	orgSlug string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	db, err := testInfra.CloneTestDatabase(t, "marketplacetokenbackfill")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	orgSlug := "marketplace-token-backfill-" + uuid.NewString()
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Marketplace Token Backfill Org",
		Slug:        orgSlug,
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	return &fixture{db: db, orgID: orgID, orgSlug: orgSlug}
}

// legacyConnection stores a connection the way a publish did before tokens
// were hashed: no organization_id, the token in plaintext. organizationID is
// the project's organization, which need not have a metadata row.
func (f *fixture) legacyConnection(t *testing.T, organizationID string, token string) uuid.UUID {
	t.Helper()

	project, err := projectsrepo.New(f.db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           "Backfill Project",
		Slug:           "backfill-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	conn, err := repo.New(f.db).UpsertGitHubConnection(t.Context(), repo.UpsertGitHubConnectionParams{
		ProjectID:                project.ID,
		OrganizationID:           pgtype.Text{String: "", Valid: false},
		InstallationID:           1,
		RepoOwner:                "backfill-owner",
		RepoName:                 "backfill-" + uuid.NewString(),
		PublishedMcpFingerprints: nil,
		PublishedHooksVersion:    pgtype.Text{String: "", Valid: false},
		PublishedHooksConfig:     nil,
	})
	require.NoError(t, err)

	require.NoError(t, repo.New(f.db).PlantMarketplaceTokenFixture(t.Context(), repo.PlantMarketplaceTokenFixtureParams{
		OrganizationID: pgtype.Text{String: "", Valid: false},
		LegacyToken:    conv.ToPGText(token),
		TokenHash:      pgtype.Text{String: "", Valid: false},
		PlatformCopy:   pgtype.Text{String: "", Valid: false},
		OrgCopy:        pgtype.Text{String: "", Valid: false},
		ID:             conn.ID,
	}))

	return conn.ID
}

func (f *fixture) tokens(t *testing.T) *marketplacetokens.Tokens {
	t.Helper()

	return marketplacetokens.WithoutOrganizationEncryption(testenv.NewLogger(t), f.db, testenv.NewEncryptionClient(t))
}

func (f *fixture) stored(t *testing.T, id uuid.UUID) repo.PluginGithubConnection {
	t.Helper()

	conn, err := repo.New(f.db).GetGitHubConnectionRowFixture(t.Context(), id)
	require.NoError(t, err)

	return conn
}

func TestDryRunCountsAndWritesNothing(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	id := f.legacyConnection(t, f.orgID, "legacy-token")

	report, err := marketplacetokenbackfill.Run(t.Context(), f.db, nil, marketplacetokenbackfill.Options{Apply: false, BatchSize: 10})
	require.NoError(t, err)
	require.Equal(t, marketplacetokenbackfill.Report{
		Attributed:           0,
		Moved:                0,
		Skipped:              0,
		NeedsOrganizationKey: 0,
		Counts:               marketplacetokenbackfill.Counts{Unattributed: 1, Legacy: 1, Hashed: 0, MissingOrganization: 0},
	}, report)
	require.Equal(t, "legacy-token", f.stored(t, id).MarketplaceToken.String)
}

func TestApplyMovesEveryPlaintextTokenAndKeepsTheURLResolving(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	ids := []uuid.UUID{
		f.legacyConnection(t, f.orgID, "legacy-token-one"),
		f.legacyConnection(t, f.orgID, "legacy-token-two"),
		f.legacyConnection(t, f.orgID, "legacy-token-three"),
	}
	before := f.stored(t, ids[0])

	// A batch smaller than the work exercises paging and repeated batches.
	report, err := marketplacetokenbackfill.Run(t.Context(), f.db, f.tokens(t), marketplacetokenbackfill.Options{Apply: true, BatchSize: 2})
	require.NoError(t, err)
	require.Equal(t, int64(3), report.Attributed)
	require.Equal(t, int64(3), report.Moved)
	require.Equal(t, marketplacetokenbackfill.Counts{Unattributed: 0, Legacy: 0, Hashed: 3, MissingOrganization: 0}, report.Counts)

	moved := f.stored(t, ids[0])
	require.Equal(t, f.orgID, moved.OrganizationID.String)
	require.False(t, moved.MarketplaceToken.Valid, "the plaintext token is gone")
	require.Equal(t, before.UpdatedAt, moved.UpdatedAt, "the backfill leaves the last-published timestamp alone")

	resolved, err := repo.New(f.db).GetGitHubConnectionByMarketplaceTokenHash(t.Context(), conv.ToPGText(marketplacetokens.Hash("legacy-token-one")))
	require.NoError(t, err)
	require.Equal(t, ids[0], resolved.ID, "the install URL devices already hold keeps resolving")

	token, err := f.tokens(t).Open(t.Context(), f.db, marketplacetokens.FromConnection(moved))
	require.NoError(t, err)
	require.Equal(t, "legacy-token-one", token)

	again, err := marketplacetokenbackfill.Run(t.Context(), f.db, f.tokens(t), marketplacetokenbackfill.Options{Apply: true, BatchSize: 2})
	require.NoError(t, err)
	require.Zero(t, again.Attributed+again.Moved, "a second run finds nothing to do")
}

func TestApplyLeavesAConnectionWithoutOrganizationMetadataInPlace(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	id := f.legacyConnection(t, "org_without_metadata_"+uuid.NewString(), "orphan-token")

	report, err := marketplacetokenbackfill.Run(t.Context(), f.db, f.tokens(t), marketplacetokenbackfill.Options{Apply: true, BatchSize: 10})
	require.NoError(t, err)
	require.Zero(t, report.Attributed)
	require.Equal(t, marketplacetokenbackfill.Counts{Unattributed: 0, Legacy: 0, Hashed: 0, MissingOrganization: 1}, report.Counts, "it counts only as missing an organization, not as work an apply can do")
	require.Equal(t, "orphan-token", f.stored(t, id).MarketplaceToken.String, "the token keeps resolving through the plaintext fallback")
}

func TestApplyCountsTokensWhoseOrganizationMovedPastThePlatformKey(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	id := f.legacyConnection(t, f.orgID, "enrolled-token")

	keys := orgencryptiontest.New(t, f.db)
	transitions := orgencryption.NewTransitions(testenv.NewLogger(t), f.db, audit.NewLogger(), keys.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	_, err := transitions.Enroll(t.Context(), f.orgID, orgencryption.UseCaseMarketplaceTokens, orgencryption.SystemActor())
	require.NoError(t, err)

	report, err := marketplacetokenbackfill.Run(t.Context(), f.db, f.tokens(t), marketplacetokenbackfill.Options{Apply: true, BatchSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), report.NeedsOrganizationKey)
	require.Zero(t, report.Moved)
	require.Equal(t, "enrolled-token", f.stored(t, id).MarketplaceToken.String, "nothing falls back to the platform key")
}

func TestApplyForOneOrganizationLeavesTheOthers(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	otherOrgID := "org_" + uuid.NewString()
	otherSlug := "another-backfill-" + uuid.NewString()
	_, err := orgrepo.New(f.db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          otherOrgID,
		Name:        "Another Backfill Org",
		Slug:        otherSlug,
		WorkosID:    conv.ToPGText(otherOrgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	mine := f.legacyConnection(t, f.orgID, "my-token")
	other := f.legacyConnection(t, otherOrgID, "other-token")
	f.legacyConnection(t, "org_without_metadata_"+uuid.NewString(), "orphan-token")

	organizations, err := marketplacetokenbackfill.Organizations(t.Context(), f.db)
	require.NoError(t, err)
	require.Equal(t, []marketplacetokenbackfill.Organization{
		{ID: otherOrgID, Name: "Another Backfill Org", Slug: otherSlug, Unattributed: 1, Legacy: 1},
		{ID: f.orgID, Name: "Marketplace Token Backfill Org", Slug: f.orgSlug, Unattributed: 1, Legacy: 1},
	}, organizations, "organizations come by name, and one without a metadata row is left out")

	preview, err := marketplacetokenbackfill.Run(t.Context(), f.db, nil, marketplacetokenbackfill.Options{OrganizationID: f.orgID, Apply: false, BatchSize: 10})
	require.NoError(t, err)
	require.Equal(t, marketplacetokenbackfill.Counts{Unattributed: 1, Legacy: 1, Hashed: 0, MissingOrganization: 0}, preview.Counts, "a connection without an organization_id counts under its project's organization")

	report, err := marketplacetokenbackfill.Run(t.Context(), f.db, f.tokens(t), marketplacetokenbackfill.Options{OrganizationID: f.orgID, Apply: true, BatchSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), report.Attributed)
	require.Equal(t, int64(1), report.Moved)
	require.Equal(t, marketplacetokenbackfill.Counts{Unattributed: 0, Legacy: 0, Hashed: 1, MissingOrganization: 0}, report.Counts, "the counts cover only that organization")

	require.False(t, f.stored(t, mine).MarketplaceToken.Valid)
	untouched := f.stored(t, other)
	require.Equal(t, "other-token", untouched.MarketplaceToken.String, "another organization's token stays in place")
	require.False(t, untouched.OrganizationID.Valid, "another organization's connection is not attributed")

	organizations, err = marketplacetokenbackfill.Organizations(t.Context(), f.db)
	require.NoError(t, err)
	require.Len(t, organizations, 1)
	require.Equal(t, otherOrgID, organizations[0].ID)

	all, err := marketplacetokenbackfill.Run(t.Context(), f.db, nil, marketplacetokenbackfill.Options{OrganizationID: "", Apply: false, BatchSize: 10})
	require.NoError(t, err)
	require.Equal(t, marketplacetokenbackfill.Counts{Unattributed: 1, Legacy: 1, Hashed: 1, MissingOrganization: 1}, all.Counts, "without an organization the counts cover all of them")
}
