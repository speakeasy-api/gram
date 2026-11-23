package marketplace_test

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/marketplace"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins/marketplacetokens"
	"github.com/speakeasy-api/gram/server/internal/plugins/marketplacetokens/marketplacetokenstest"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
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

// newConnection creates an organization, a project and its GitHub connection
// without a marketplace token, and returns the database and project.
func newConnection(t *testing.T) (*pgxpool.Pool, uuid.UUID) {
	t.Helper()

	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Marketplace Resolver Org",
		Slug:        "marketplace-resolver-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           "Marketplace Resolver Project",
		Slug:           "marketplace-resolver-" + uuid.NewString()[:8],
		OrganizationID: orgID,
	})
	require.NoError(t, err)

	_, err = pluginsrepo.New(conn).UpsertGitHubConnection(t.Context(), pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID:                project.ID,
		OrganizationID:           conv.ToPGText(orgID),
		InstallationID:           1,
		RepoOwner:                "resolver-owner",
		RepoName:                 "resolver-" + uuid.NewString(),
		PublishedMcpFingerprints: nil,
		PublishedHooksVersion:    pgtype.Text{String: "", Valid: false},
		PublishedHooksConfig:     nil,
	})
	require.NoError(t, err)

	return conn, project.ID
}

func TestDBResolverResolvesAHashedToken(t *testing.T) {
	t.Parallel()

	conn, projectID := newConnection(t)
	token, err := marketplacetokens.Mint()
	require.NoError(t, err)
	marketplacetokenstest.Plant(t, conn, projectID, token)

	upstream, err := marketplace.NewLocalDBResolver(testenv.NewLogger(t), conn).Resolve(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, "resolver-owner", upstream.Owner)
}

func TestDBResolverResolvesAPlaintextTokenTheBackfillHasNotMoved(t *testing.T) {
	t.Parallel()

	conn, projectID := newConnection(t)
	token, err := marketplacetokens.Mint()
	require.NoError(t, err)
	marketplacetokenstest.PlantLegacy(t, conn, projectID, token)

	upstream, err := marketplace.NewLocalDBResolver(testenv.NewLogger(t), conn).Resolve(t.Context(), token)
	require.NoError(t, err, "install URLs handed out before tokens were hashed keep working")
	require.Equal(t, "resolver-owner", upstream.Owner)
}

func TestDBResolverRejectsAnUnknownToken(t *testing.T) {
	t.Parallel()

	conn, projectID := newConnection(t)
	token, err := marketplacetokens.Mint()
	require.NoError(t, err)
	marketplacetokenstest.Plant(t, conn, projectID, token)
	other, err := marketplacetokens.Mint()
	require.NoError(t, err)

	_, err = marketplace.NewLocalDBResolver(testenv.NewLogger(t), conn).Resolve(t.Context(), other)
	require.ErrorIs(t, err, marketplace.ErrNotFound)

	// A token's hash is not itself a token: a URL built from a leaked hash
	// does not resolve.
	_, err = marketplace.NewLocalDBResolver(testenv.NewLogger(t), conn).Resolve(t.Context(), marketplacetokens.Hash(token))
	require.ErrorIs(t, err, marketplace.ErrNotFound)
}
