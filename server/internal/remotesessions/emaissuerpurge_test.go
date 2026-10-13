package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestEMAIssuerTokenPurger_OrganizationAndProviderScope(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "issuer_token_purge")
	require.NoError(t, err)
	fixtures := testrepo.New(db)
	q := repo.New(db)
	targetOrg, otherOrg := "org-"+uuid.NewString(), "org-"+uuid.NewString()
	for _, org := range []string{targetOrg, otherOrg} {
		require.NoError(t, fixtures.SeedDelegationLoaderOrganizationFixture(ctx, testrepo.SeedDelegationLoaderOrganizationFixtureParams{OrganizationID: org, Name: "Purge test", Slug: org}))
	}
	projects := []struct {
		org string
		id  uuid.UUID
	}{{targetOrg, uuid.New()}, {targetOrg, uuid.New()}, {otherOrg, uuid.New()}}
	for _, project := range projects {
		_, err := fixtures.CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: project.id, OrganizationID: project.org, Name: "Purge test", Slug: project.id.String()})
		require.NoError(t, err)
		for _, provider := range []string{"speakeasy", "external", "unknown"} {
			for _, retired := range []bool{false, true} {
				_, err := q.CreateEMAIssuerPurgeCredentialFixture(ctx, repo.CreateEMAIssuerPurgeCredentialFixtureParams{
					OrganizationID: pgtype.Text{String: project.org, Valid: true}, ProjectID: uuid.NullUUID{UUID: project.id, Valid: true},
					SubjectUrn: "user:" + uuid.NewString(), EmaProvider: provider, Retired: retired,
				})
				require.NoError(t, err)
			}
		}
	}
	purger := remotesessions.NewEMAIssuerTokenPurger(db)
	for range 2 {
		require.NoError(t, purger.PurgeEMAIssuerTokens(ctx, targetOrg), "purge is idempotent")
	}
	for _, project := range projects {
		rows, err := q.ListEMAIssuerPurgeCredentialsFixture(ctx, uuid.NullUUID{UUID: project.id, Valid: true})
		require.NoError(t, err)
		require.Len(t, rows, 6)
		retiredCounts := make(map[string]int)
		for _, row := range rows {
			if row.Deleted {
				retiredCounts[row.EmaProvider]++
			}
			purged := project.org == targetOrg && row.EmaProvider == "speakeasy"
			if purged {
				require.True(t, row.Deleted)
				require.False(t, row.AccessTokenEncrypted.Valid)
			} else {
				require.True(t, row.AccessTokenEncrypted.Valid)
				require.Equal(t, "encrypted-fixture", row.AccessTokenEncrypted.String, "other tenants and providers retain their credentials")
			}
		}
		require.Equal(t, 1, retiredCounts["external"])
		require.Equal(t, 1, retiredCounts["unknown"])
		if project.org == targetOrg {
			require.Equal(t, 2, retiredCounts["speakeasy"])
		} else {
			require.Equal(t, 1, retiredCounts["speakeasy"])
		}
	}
}
