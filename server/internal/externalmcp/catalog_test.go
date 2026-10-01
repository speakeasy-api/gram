package externalmcp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/feature"
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

	for _, mutate := range []func(*repo.ListMCPRegistriesRow){
		func(row *repo.ListMCPRegistriesRow) {
			row.SourceType = pgtype.Text{String: registrySourceTypePulseV01, Valid: true}
		},
		func(row *repo.ListMCPRegistriesRow) {
			row.AuthProfile = pgtype.Text{String: registryAuthProfilePulseServerCredentials, Valid: true}
		},
		func(row *repo.ListMCPRegistriesRow) { row.Enabled = pgtype.Bool{Bool: false, Valid: true} },
		func(row *repo.ListMCPRegistriesRow) {
			row.CertificationState = pgtype.Text{String: "pending", Valid: true}
		},
		func(row *repo.ListMCPRegistriesRow) {
			row.CertificationVersion = pgtype.Text{String: "v1", Valid: true}
		},
		func(row *repo.ListMCPRegistriesRow) { row.Priority = pgtype.Int4{Int32: 1, Valid: true} },
		func(row *repo.ListMCPRegistriesRow) { row.SourceKey = pgtype.Text{String: "partial", Valid: true} },
	} {
		row := legacyPulse
		mutate(&row)
		_, ok := catalogSourceFromRow(row)
		require.False(t, ok)
	}
}

type routingRepo struct{ rows []repo.ListMCPRegistriesRow }

func (r routingRepo) ListMCPRegistries(context.Context) ([]repo.ListMCPRegistriesRow, error) {
	return r.rows, nil
}
func (r routingRepo) GetMCPRegistryByID(_ context.Context, id uuid.UUID) (repo.GetMCPRegistryByIDRow, error) {
	for _, row := range r.rows {
		if row.ID == id {
			return repo.GetMCPRegistryByIDRow(row), nil
		}
	}
	return repo.GetMCPRegistryByIDRow{}, errors.New("missing")
}

type routingFlags struct {
	feature.Provider
	on  bool
	err error
}

func (f routingFlags) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return f.on, f.err
}
func TestCatalogSelectsExactlyOneSource(t *testing.T) {
	t.Parallel()
	pulseID := uuid.New()
	rows := []repo.ListMCPRegistriesRow{
		{ID: pulseID, Url: "https://api.pulsemcp.com"},
		{ID: NativeCatalogRegistryID, Url: NativeCatalogRegistryURL, SourceType: pgtype.Text{String: registrySourceTypeNative, Valid: true}, AuthProfile: pgtype.Text{String: registryAuthProfileNone, Valid: true}, Enabled: pgtype.Bool{Bool: true, Valid: true}, CertificationState: pgtype.Text{String: "certified", Valid: true}, SourceKey: pgtype.Text{String: "speakeasy", Valid: true}},
	}
	for _, tc := range []struct {
		name string
		on   bool
		err  error
		want uuid.UUID
	}{
		{name: "off", want: pulseID}, {name: "on", on: true, want: NativeCatalogRegistryID}, {name: "error", on: true, err: errors.New("flags down"), want: pulseID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := NewCatalogService(nil, &RegistryClient{}, &NativeRegistryReader{}, routingFlags{on: tc.on, err: tc.err})
			s.repo = routingRepo{rows}
			source, err := s.SelectedSource(t.Context(), "org-id", "org-slug")
			require.NoError(t, err)
			require.Equal(t, tc.want, source.Registry.ID)
			wrong := pulseID
			if wrong == tc.want {
				wrong = NativeCatalogRegistryID
			}
			_, err = s.SelectSource(t.Context(), "org-id", "org-slug", wrong)
			require.ErrorIs(t, err, ErrCatalogSourceNotFound)
			_, err = s.IdentitySource(t.Context(), wrong)
			require.NoError(t, err)
		})
	}
	s := NewCatalogService(nil, &RegistryClient{}, &NativeRegistryReader{}, routingFlags{on: true})
	s.repo = routingRepo{rows: rows[:1]}
	_, err := s.SelectedSource(t.Context(), "org-id", "org-slug")
	require.ErrorIs(t, err, ErrCatalogSourceNotFound)
}

func TestDashboardDetailsRetainEveryRemote(t *testing.T) {
	t.Parallel()
	result, err := decodeDashboardDetails([]byte(`{"server":{"name":"example/server","version":"1","description":"full","title":"Full title","icons":[{"src":"https://example.test/icon.png"}],"remotes":[{"type":"sse","url":"https://example.test/sse"},{"type":"streamable-http","url":"https://example.test/mcp","headers":[{"name":"X-Token","description":"token","isRequired":true}]}]},"_meta":{"com.pulsemcp/server-version":{"remotes[1]":{"tools":[{"name":"search","description":"find","inputSchema":{"type":"object"}}]}}}}`), true)
	require.NoError(t, err)
	require.Len(t, result.Remotes, 2)
	require.Len(t, result.Remotes[1].Headers, 1)
	require.Len(t, result.Tools, 1)
	require.Equal(t, "Full title", *result.Title)
	require.Equal(t, "https://example.test/icon.png", *result.IconURL)
	require.Contains(t, result.Meta, "com.pulsemcp/server-version")
}

type failedCatalogReader struct{ calls int }

func (r *failedCatalogReader) ListServers(context.Context, Registry, ListServersParams) (ListServersResult, error) {
	r.calls++
	return ListServersResult{}, errors.New("selected source failed")
}
func (r *failedCatalogReader) GetServerDetails(context.Context, Registry, string, []string) (*ServerDetails, error) {
	r.calls++
	return nil, errors.New("selected source failed")
}
func TestCatalogSelectedFailureNeverFallsBack(t *testing.T) {
	t.Parallel()
	pulse, native := &failedCatalogReader{}, &failedCatalogReader{}
	s := NewCatalogService(nil, pulse, native, routingFlags{on: true})
	s.repo = routingRepo{rows: []repo.ListMCPRegistriesRow{
		{ID: uuid.New(), Url: "https://api.pulsemcp.com"},
		{ID: NativeCatalogRegistryID, Url: NativeCatalogRegistryURL, SourceType: pgtype.Text{String: registrySourceTypeNative, Valid: true}, AuthProfile: pgtype.Text{String: registryAuthProfileNone, Valid: true}, Enabled: pgtype.Bool{Bool: true, Valid: true}, CertificationState: pgtype.Text{String: "certified", Valid: true}, SourceKey: pgtype.Text{String: "speakeasy", Valid: true}},
	}}
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org-id", OrganizationSlug: "org-slug"})
	_, err := s.List(ctx, nil, nil)
	require.ErrorContains(t, err, "selected source failed")
	require.Equal(t, 1, native.calls)
	require.Zero(t, pulse.calls)
	_, err = s.IdentityDetails(t.Context(), NativeCatalogRegistryID, "example/server", nil)
	require.ErrorContains(t, err, "selected source failed")
	require.Equal(t, 2, native.calls)
	require.Zero(t, pulse.calls)
}
