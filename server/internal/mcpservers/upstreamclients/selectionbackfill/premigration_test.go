package selectionbackfill

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/associationbackfill"
)

// migrate runs the organization's migration as the admin dashboard does:
// associations first, then every live credential's choices.
func (f *fixture) migrate() Report {
	f.t.Helper()
	_, err := associationbackfill.Run(f.t.Context(), f.pool, associationbackfill.Options{OrganizationID: f.orgID, Apply: true, PageSize: 0})
	require.NoError(f.t, err)
	report, err := Run(f.t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: true, PageSize: 0})
	require.NoError(f.t, err)
	return report
}

// A credential that signs in before its organization is migrated records
// no-auth for a server whose client is not associated yet. Migrating the
// organization associates the client, and must leave the credential with a
// usable choice of it rather than a stale no-auth one.
func TestMigrationUpgradesAPreMigrationNoAuthChoice(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuer := f.projectIssuer()
	clientID := f.client(issuer)
	server := f.server(issuer)
	credential := f.session(issuer)

	f.record(issuer, credential)
	require.Equal(t, map[uuid.UUID]uuid.UUID{server: uuid.Nil}, f.selections(credential), "before migration the server has no associated client")

	preview, err := associationbackfill.Run(t.Context(), f.pool, associationbackfill.Options{OrganizationID: f.orgID, Apply: true, PageSize: 0})
	require.NoError(t, err)
	require.Equal(t, 1, preview.Written)
	coverage, err := Run(t.Context(), f.pool, Options{OrganizationID: f.orgID, Apply: false, PageSize: 0})
	require.NoError(t, err)
	require.Equal(t, 1, coverage.Pairs[OutcomeClient], "a no-auth choice the server's new client makes unusable is still to record")
	require.Zero(t, coverage.Pairs[OutcomeSelected])

	report := f.migrate()
	require.Empty(t, report.Gaps)
	require.Equal(t, map[uuid.UUID]uuid.UUID{server: clientID}, f.selections(credential))
	got := f.selection(issuer, credential, server)
	require.True(t, got.Usable)
	require.False(t, got.NoAuth)
}

// When the migration leaves a pre-migration no-auth choice's server with
// several clients, nothing decides between them: the choice stays unusable
// and is reported as a reconnection, never counted as selected.
func TestMigrationReportsAPreMigrationNoAuthChoiceItCannotDecide(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuer := f.projectIssuer()
	f.client(issuer)
	f.client(issuer)
	server := f.server(issuer)
	credential := f.session(issuer)

	f.record(issuer, credential)
	require.Equal(t, map[uuid.UUID]uuid.UUID{server: uuid.Nil}, f.selections(credential))

	report := f.migrate()
	require.Equal(t, []Gap{{CredentialID: credential, MCPServerID: server, Outcome: OutcomeAmbiguous}}, report.Gaps)
	require.False(t, f.selection(issuer, credential, server).Usable)
}

// Only an unusable no-auth choice is rewritten: a usable no-auth choice stays,
// and a client choice is never replaced by another client.
func TestMigrationKeepsUsableChoices(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	issuer := f.projectIssuer()
	first := f.client(issuer)
	chosen := f.server(issuer, first)
	anonymous := f.server(issuer)
	credential := f.session(issuer)

	f.record(issuer, credential)
	require.Equal(t, map[uuid.UUID]uuid.UUID{chosen: first, anonymous: uuid.Nil}, f.selections(credential))

	f.associate(chosen, f.client(issuer))
	f.migrate()
	require.Equal(t, map[uuid.UUID]uuid.UUID{chosen: first, anonymous: uuid.Nil}, f.selections(credential))
}
