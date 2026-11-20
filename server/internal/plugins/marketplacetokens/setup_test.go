package marketplacetokens_test

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
	"github.com/speakeasy-api/gram/server/internal/encryption"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/orgencryption/orgencryptiontest"
	orgencryptionrepo "github.com/speakeasy-api/gram/server/internal/orgencryption/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/marketplacetokens"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
		os.Exit(1)
	}

	infra = res

	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
		os.Exit(1)
	}

	os.Exit(code)
}

type testInstance struct {
	conn        *pgxpool.Pool
	orgID       string
	fixture     *orgencryptiontest.Fixture
	platform    *encryption.Client
	transitions *orgencryption.Transitions
	cipher      *orgencryption.Cipher
	tokens      *marketplacetokens.Tokens
}

func newTestInstance(t *testing.T) *testInstance {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgID := createOrganization(t, conn)

	fixture := orgencryptiontest.New(t, conn)
	platform := testenv.NewEncryptionClient(t)
	transitions := orgencryption.NewTransitions(testenv.NewLogger(t), conn, audit.NewLogger(), fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	cipher := orgencryption.NewCipher(testenv.NewLogger(t), platform, fixture.Service, transitions)

	return &testInstance{
		conn:        conn,
		orgID:       orgID,
		fixture:     fixture,
		platform:    platform,
		transitions: transitions,
		cipher:      cipher,
		tokens:      marketplacetokens.New(testenv.NewLogger(t), cipher, platform),
	}
}

func createOrganization(t *testing.T, conn *pgxpool.Pool) string {
	t.Helper()

	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Marketplace Tokens Test Org",
		Slug:        "marketplace-tokens-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	return orgID
}

// createConnection creates a project in the organization and its GitHub
// connection, with no marketplace token, as a publish's upsert stores it.
func (ti *testInstance) createConnection(t *testing.T, organizationID string) repo.PluginGithubConnection {
	t.Helper()

	project, err := projectsrepo.New(ti.conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           "Marketplace Tokens Test Project",
		Slug:           "marketplace-tokens-" + uuid.NewString()[:8],
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	conn, err := repo.New(ti.conn).UpsertGitHubConnection(t.Context(), repo.UpsertGitHubConnectionParams{
		ProjectID:                project.ID,
		OrganizationID:           conv.ToPGText(organizationID),
		InstallationID:           1,
		RepoOwner:                "marketplace-owner",
		RepoName:                 "marketplace-" + uuid.NewString(),
		PublishedMcpFingerprints: nil,
		PublishedHooksVersion:    pgtype.Text{String: "", Valid: false},
		PublishedHooksConfig:     nil,
	})
	require.NoError(t, err)

	return conn
}

// plantLegacy stores token in plaintext, as a publish did before tokens were
// hashed. withOrganization false leaves organization_id unset, as on a
// connection stored before that column existed.
func (ti *testInstance) plantLegacy(t *testing.T, conn repo.PluginGithubConnection, token string, withOrganization bool) {
	t.Helper()

	organizationID := conn.OrganizationID
	if !withOrganization {
		organizationID = pgtype.Text{String: "", Valid: false}
	}

	require.NoError(t, repo.New(ti.conn).PlantMarketplaceTokenFixture(t.Context(), repo.PlantMarketplaceTokenFixtureParams{
		OrganizationID: organizationID,
		LegacyToken:    conv.ToPGText(token),
		TokenHash:      pgtype.Text{String: "", Valid: false},
		PlatformCopy:   pgtype.Text{String: "", Valid: false},
		OrgCopy:        pgtype.Text{String: "", Valid: false},
		ID:             conn.ID,
	}))
}

// enroll moves the test organization's marketplace tokens to dual_write.
func (ti *testInstance) enroll(t *testing.T) {
	t.Helper()

	_, err := ti.transitions.Enroll(t.Context(), ti.orgID, orgencryption.UseCaseMarketplaceTokens, orgencryption.SystemActor())
	require.NoError(t, err)
}

// runner backfills and verifies the organization-key copies.
func (ti *testInstance) run(t *testing.T) orgencryption.RunResult {
	t.Helper()

	result, err := orgencryption.NewRunner(testenv.NewLogger(t), ti.conn, ti.cipher, testenv.NewMeterProvider(t)).Run(t.Context(), ti.orgID, orgencryption.UseCaseMarketplaceTokens, []orgencryption.CopyStore{marketplacetokens.NewCopyStore(ti.conn)})
	require.NoError(t, err)

	return result
}

// finalize moves the test organization's marketplace tokens from dual_write to
// org_kms and clears their platform-key copies.
func (ti *testInstance) finalize(t *testing.T) {
	t.Helper()

	stores := []orgencryption.CopyStore{marketplacetokens.NewCopyStore(ti.conn)}
	ti.run(t)

	err := orgencryptionrepo.New(ti.conn).SetEncryptionTransitionStateChangedAt(t.Context(), orgencryptionrepo.SetEncryptionTransitionStateChangedAtParams{
		StateChangedAt: pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
		OrganizationID: ti.orgID,
		UseCase:        string(orgencryption.UseCaseMarketplaceTokens),
	})
	require.NoError(t, err)

	_, err = ti.transitions.Finalize(t.Context(), ti.orgID, orgencryption.UseCaseMarketplaceTokens, orgencryption.FinalizeRequest{MinimumDualWrite: time.Minute, Stores: stores}, orgencryption.SystemActor())
	require.NoError(t, err)

	_, err = ti.transitions.ClearPlatformCopies(t.Context(), ti.orgID, orgencryption.UseCaseMarketplaceTokens, stores, orgencryption.SystemActor())
	require.NoError(t, err)
}

// stored reads the connection back from the database.
func (ti *testInstance) stored(t *testing.T, id uuid.UUID) repo.PluginGithubConnection {
	t.Helper()

	conn, err := repo.New(ti.conn).GetGitHubConnectionRowFixture(t.Context(), id)
	require.NoError(t, err)

	return conn
}

// otherProcessTokens is Tokens in another process: same database and keys,
// its own transition state cache.
func (ti *testInstance) otherProcessTokens(t *testing.T) *marketplacetokens.Tokens {
	t.Helper()

	transitions := orgencryption.NewTransitions(testenv.NewLogger(t), ti.conn, audit.NewLogger(), ti.fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
	return marketplacetokens.New(testenv.NewLogger(t), orgencryption.NewCipher(testenv.NewLogger(t), ti.platform, ti.fixture.Service, transitions), ti.platform)
}

// otherTransitions is transition state in another process.
func (ti *testInstance) otherTransitions(t *testing.T) *orgencryption.Transitions {
	t.Helper()

	return orgencryption.NewTransitions(testenv.NewLogger(t), ti.conn, audit.NewLogger(), ti.fixture.Provisioner, orgencryption.TransitionsConfig{ClearSettleDelay: time.Millisecond})
}
