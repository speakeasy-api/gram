package externalmcp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
		SourceType:         pgtype.Text{String: "unsupported_v1", Valid: true},
		AuthProfile:        pgtype.Text{String: "unsupported_auth", Valid: true},
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

func TestCatalogSourceFromRowRejectsMissingMetadata(t *testing.T) {
	t.Parallel()
	_, ok := catalogSourceFromRow(repo.ListMCPRegistriesRow{ID: uuid.New(), Url: "https://registry.example.test"})
	require.False(t, ok)
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
	legacyID := uuid.New()
	rows := []repo.ListMCPRegistriesRow{
		{ID: legacyID, Url: "https://api.pulsemcp.com"},
		{ID: NativeCatalogRegistryID, Url: NativeCatalogRegistryURL, SourceType: pgtype.Text{String: registrySourceTypeNative, Valid: true}, AuthProfile: pgtype.Text{String: registryAuthProfileNone, Valid: true}, Enabled: pgtype.Bool{Bool: true, Valid: true}, CertificationState: pgtype.Text{String: "certified", Valid: true}, SourceKey: pgtype.Text{String: "speakeasy", Valid: true}},
	}
	for _, flags := range []feature.Provider{nil, routingFlags{}, routingFlags{on: true}, routingFlags{err: errors.New("flags down")}} {
		s := NewCatalogService(nil, &RegistryClient{}, &NativeRegistryReader{}, flags)
		s.repo = routingRepo{rows}
		source, err := s.SelectedSource(t.Context(), "org-id", "org-slug")
		require.NoError(t, err)
		require.Equal(t, NativeCatalogRegistryID, source.Registry.ID)
		_, err = s.SelectSource(t.Context(), "org-id", "org-slug", legacyID)
		require.ErrorIs(t, err, ErrCatalogSourceNotFound)
		_, err = s.IdentitySource(t.Context(), NativeCatalogRegistryID)
		require.NoError(t, err)
		_, err = s.IdentitySource(t.Context(), legacyID)
		require.ErrorIs(t, err, ErrUnsupportedCatalogSource)
		s.repo = routingRepo{rows: rows[:1]}
		_, err = s.SelectedSource(t.Context(), "org-id", "org-slug")
		require.ErrorIs(t, err, ErrCatalogSourceNotFound)
	}
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
	legacy, native := &failedCatalogReader{}, &failedCatalogReader{}
	s := NewCatalogService(nil, legacy, native)
	s.repo = routingRepo{rows: []repo.ListMCPRegistriesRow{
		{ID: uuid.New(), Url: "https://api.pulsemcp.com"},
		{ID: NativeCatalogRegistryID, Url: NativeCatalogRegistryURL, SourceType: pgtype.Text{String: registrySourceTypeNative, Valid: true}, AuthProfile: pgtype.Text{String: registryAuthProfileNone, Valid: true}, Enabled: pgtype.Bool{Bool: true, Valid: true}, CertificationState: pgtype.Text{String: "certified", Valid: true}, SourceKey: pgtype.Text{String: "speakeasy", Valid: true}},
	}}
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org-id", OrganizationSlug: "org-slug"})
	_, err := s.List(ctx, nil, nil)
	require.ErrorContains(t, err, "selected source failed")
	require.Equal(t, 1, native.calls)
	require.Zero(t, legacy.calls)
	_, err = s.IdentityDetails(t.Context(), NativeCatalogRegistryID, "example/server", nil)
	require.ErrorContains(t, err, "selected source failed")
	require.Equal(t, 2, native.calls)
	require.Zero(t, legacy.calls)
}

func TestCatalogUnsupportedIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		id          uuid.UUID
		url         string
		sourceType  string
		authProfile string
	}{
		{name: "missing metadata", id: NativeCatalogRegistryID, url: "https://registry.example.test"},
		{name: "unknown source type", id: NativeCatalogRegistryID, sourceType: "unsupported_v1", authProfile: "none"},
		{name: "unknown auth profile", id: NativeCatalogRegistryID, sourceType: "native_v1", authProfile: "unsupported_auth"},
		{name: "missing auth profile", id: NativeCatalogRegistryID, sourceType: "native_v1"},
		{name: "native profile wrong namespace", id: uuid.New(), sourceType: "native_v1", authProfile: "none"},
	} {
		for _, state := range []string{"enabled", "disabled", "uncertified"} {
			t.Run(tc.name+"/"+state, func(t *testing.T) {
				t.Parallel()
				row := repo.ListMCPRegistriesRow{
					ID: tc.id, Url: tc.url,
					SourceType:         pgtype.Text{String: tc.sourceType, Valid: tc.sourceType != ""},
					AuthProfile:        pgtype.Text{String: tc.authProfile, Valid: tc.authProfile != ""},
					Enabled:            pgtype.Bool{Bool: state != "disabled", Valid: true},
					CertificationState: pgtype.Text{String: "certified", Valid: true},
				}
				if state == "uncertified" {
					row.CertificationState.String = "pending"
				}
				s := NewCatalogService(nil, nil, &NativeRegistryReader{})
				s.repo = routingRepo{rows: []repo.ListMCPRegistriesRow{row}}
				_, err := s.IdentitySource(t.Context(), row.ID)
				require.ErrorIs(t, err, ErrUnsupportedCatalogSource)
			})
		}
	}
}

func TestCatalogNativeIdentityAdmission(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"enabled", "disabled", "uncertified", "wrong namespace", "wrong profile"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			row := repo.ListMCPRegistriesRow{
				ID: NativeCatalogRegistryID, Url: NativeCatalogRegistryURL,
				SourceType:         pgtype.Text{String: registrySourceTypeNative, Valid: true},
				AuthProfile:        pgtype.Text{String: registryAuthProfileNone, Valid: true},
				Enabled:            pgtype.Bool{Bool: true, Valid: true},
				CertificationState: pgtype.Text{String: "certified", Valid: true},
				SourceKey:          pgtype.Text{String: "speakeasy", Valid: true},
			}
			var want error
			switch state {
			case "disabled":
				row.Enabled.Bool = false
				want = ErrCatalogSourceDisabled
			case "uncertified":
				row.CertificationState.String = "pending"
				want = ErrCatalogSourceDisabled
			case "wrong namespace":
				row.ID = uuid.New()
				want = ErrUnsupportedCatalogSource
			case "wrong profile":
				row.AuthProfile.String = "unknown"
				want = ErrUnsupportedCatalogSource
			}
			s := NewCatalogService(nil, nil, &NativeRegistryReader{})
			s.repo = routingRepo{rows: []repo.ListMCPRegistriesRow{row}}
			_, err := s.IdentitySource(t.Context(), row.ID)
			require.ErrorIs(t, err, want)
		})
	}
}

func TestCatalogDoesNotAdmitUnsupportedAdapter(t *testing.T) {
	t.Parallel()
	s := NewCatalogService(nil, nil, &NativeRegistryReader{})
	_, err := s.ReaderFor(CatalogSource{SourceType: "unsupported_v1", AuthProfile: "unsupported_auth"})
	require.ErrorIs(t, err, ErrUnknownRegistrySource)
}

type failingIdentityRepo struct {
	routingRepo
	err error
}

func (r failingIdentityRepo) GetMCPRegistryByID(context.Context, uuid.UUID) (repo.GetMCPRegistryByIDRow, error) {
	return repo.GetMCPRegistryByIDRow{}, r.err
}
func TestCatalogIdentityLookupErrors(t *testing.T) {
	t.Parallel()
	transient := errors.New("database unavailable")
	for _, tc := range []struct {
		name        string
		cause, want error
	}{
		{"missing", pgx.ErrNoRows, ErrCatalogSourceNotFound},
		{"transient", transient, transient},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := NewCatalogService(nil, nil, &NativeRegistryReader{})
			s.repo = failingIdentityRepo{err: tc.cause}
			_, err := s.IdentitySource(t.Context(), NativeCatalogRegistryID)
			require.ErrorIs(t, err, tc.want)
			require.NotErrorIs(t, err, ErrUnsupportedCatalogSource)
		})
	}
}
