// Package marketplacetokenstest wires marketplace tokens for tests and plants
// them on GitHub connections the way a publish stores them.
package marketplacetokenstest

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/plugins/marketplacetokens"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// Tokens returns the seam tests wire into services: the fixed test platform
// key, without organization encryption, so every organization in
// platform_key works.
func Tokens(t *testing.T, db *pgxpool.Pool) *marketplacetokens.Tokens {
	t.Helper()

	return marketplacetokens.WithoutOrganizationEncryption(testenv.NewLogger(t), db, testenv.NewEncryptionClient(t))
}

// Plant gives the project's GitHub connection token as its marketplace token:
// its hash and its platform-key copy, as a first publish stores them. The
// connection must exist and hold no token yet.
func Plant(t *testing.T, db *pgxpool.Pool, projectID uuid.UUID, token string) {
	t.Helper()

	conn, organizationID := attributedConnection(t, db, projectID)
	require.NoError(t, Tokens(t, db).Store(t.Context(), db, conn.ID, organizationID, token))
}

// PlantLegacy stores token in the project's GitHub connection the way a
// publish stored it before tokens were hashed: in plaintext, with no hash and
// no copies.
func PlantLegacy(t *testing.T, db *pgxpool.Pool, projectID uuid.UUID, token string) {
	t.Helper()

	conn, organizationID := attributedConnection(t, db, projectID)
	require.NoError(t, pluginsrepo.New(db).PlantMarketplaceTokenFixture(t.Context(), pluginsrepo.PlantMarketplaceTokenFixtureParams{
		OrganizationID: conv.ToPGText(organizationID),
		LegacyToken:    conv.ToPGText(token),
		TokenHash:      pgtype.Text{String: "", Valid: false},
		PlatformCopy:   pgtype.Text{String: "", Valid: false},
		OrgCopy:        pgtype.Text{String: "", Valid: false},
		ID:             conn.ID,
	}))
}

// attributedConnection returns the project's GitHub connection and its
// organization, recording the organization on a connection stored without it.
func attributedConnection(t *testing.T, db *pgxpool.Pool, projectID uuid.UUID) (pluginsrepo.PluginGithubConnection, string) {
	t.Helper()

	conn, err := pluginsrepo.New(db).GetGitHubConnection(t.Context(), projectID)
	require.NoError(t, err, "the project has no GitHub connection to plant a marketplace token on")
	require.False(t, marketplacetokens.FromConnection(conn).Has(), "the connection already holds a marketplace token")

	project, err := projectsrepo.New(db).GetProjectWithOrganizationMetadata(t.Context(), projectID)
	require.NoError(t, err)

	if !conn.OrganizationID.Valid {
		require.NoError(t, pluginsrepo.New(db).PlantMarketplaceTokenFixture(t.Context(), pluginsrepo.PlantMarketplaceTokenFixtureParams{
			OrganizationID: conv.ToPGText(project.ID),
			LegacyToken:    pgtype.Text{String: "", Valid: false},
			TokenHash:      pgtype.Text{String: "", Valid: false},
			PlatformCopy:   pgtype.Text{String: "", Valid: false},
			OrgCopy:        pgtype.Text{String: "", Valid: false},
			ID:             conn.ID,
		}))
	}

	return conn, project.ID
}
