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

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/marketplacetokenbackfill"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	"github.com/speakeasy-api/gram/server/internal/plugins/marketplacetokens"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
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
	db    *pgxpool.Pool
	orgID string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	db, err := testInfra.CloneTestDatabase(t, "marketplacetokenbackfill")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Marketplace Token Backfill Org",
		Slug:        "marketplace-token-backfill-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	return &fixture{db: db, orgID: orgID}
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
		Counts:               marketplacetokenbackfill.Counts{Unattributed: 1, Legacy: 1, Hashed: 0},
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
	require.Equal(t, marketplacetokenbackfill.Counts{Unattributed: 0, Legacy: 0, Hashed: 3}, report.Counts)

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
	require.Equal(t, marketplacetokenbackfill.Counts{Unattributed: 1, Legacy: 1, Hashed: 0}, report.Counts)
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
