package externalmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
)

func TestCatalogSourceFromRowAdmitsOnlyCertifiedReviewedSources(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	base := repo.ListMCPRegistriesRow{
		ID:                 id,
		Url:                "https://registry.example.test",
		SourceType:         pgtype.Text{String: registrySourceTypePulseV01, Valid: true},
		AuthProfile:        pgtype.Text{String: registryAuthProfilePulseServerCredentials, Valid: true},
		Enabled:            pgtype.Bool{Bool: true, Valid: true},
		CertificationState: pgtype.Text{String: registryCertificationStateCertified, Valid: true},
		SourceKey:          pgtype.Text{String: "reviewed-pulse", Valid: true},
	}

	source, ok := catalogSourceFromRow(base)
	require.True(t, ok)
	require.Equal(t, "reviewed-pulse", source.SourceKey)
	require.False(t, source.Legacy)

	for _, mutate := range []func(*repo.ListMCPRegistriesRow){
		func(row *repo.ListMCPRegistriesRow) { row.Enabled.Bool = false },
		func(row *repo.ListMCPRegistriesRow) { row.CertificationState.String = "pending" },
		func(row *repo.ListMCPRegistriesRow) { row.SourceKey = pgtype.Text{} },
		func(row *repo.ListMCPRegistriesRow) { row.AuthProfile = pgtype.Text{} },
	} {
		row := base
		mutate(&row)
		_, ok := catalogSourceFromRow(row)
		require.False(t, ok)
	}
}

func TestCatalogSourceFromRowLimitsLegacyCompatibilityToPulse(t *testing.T) {
	t.Parallel()

	legacyPulse := repo.ListMCPRegistriesRow{ID: uuid.New(), Url: "https://api.pulsemcp.com/"}
	source, ok := catalogSourceFromRow(legacyPulse)
	require.True(t, ok)
	require.True(t, source.Legacy)
	require.Equal(t, registrySourceTypePulseV01, source.SourceType)

	for _, rawURL := range []string{
		"https://registry.example.test",
		"https://api.pulsemcp.com.evil.test",
	} {
		_, ok := catalogSourceFromRow(repo.ListMCPRegistriesRow{ID: uuid.New(), Url: rawURL})
		require.False(t, ok)
	}
}
