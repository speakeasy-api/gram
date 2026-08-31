package networkingress

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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

func TestAuthorityValidateRequestRequiresSamePrivateOrigin(t *testing.T) {
	t.Parallel()
	ingressID := uuid.New()
	authority := Authority{
		Surface:          requestorigin.SurfacePrivateNetwork,
		BaseURL:          "https://private.example.ts.net",
		OrganizationID:   "org_123",
		NetworkIngressID: ingressID,
		NamespaceKind:    NamespacePlatform,
	}

	ctx := requestorigin.WithContext(context.Background(), requestorigin.Origin{
		Surface:          requestorigin.SurfacePrivateNetwork,
		BaseURL:          authority.BaseURL,
		OrganizationID:   authority.OrganizationID,
		NetworkIngressID: ingressID,
	})
	require.NoError(t, authority.ValidateRequest(ctx))
	require.Error(t, authority.ValidateRequest(context.Background()))

	platform := Authority{
		Surface:        requestorigin.SurfacePlatform,
		BaseURL:        "https://platform.example.com",
		OrganizationID: authority.OrganizationID,
		NamespaceKind:  NamespacePlatform,
	}
	require.Error(t, platform.ValidateRequest(ctx), "platform state must not continue on a private ingress")

	wrong := requestorigin.WithContext(context.Background(), requestorigin.Origin{
		Surface:          requestorigin.SurfacePrivateNetwork,
		BaseURL:          authority.BaseURL,
		OrganizationID:   authority.OrganizationID,
		NetworkIngressID: uuid.New(),
	})
	require.Error(t, authority.ValidateRequest(wrong))
}

func TestAuthorityValidateEndpointRefRejectsInconsistentCopies(t *testing.T) {
	t.Parallel()
	domainID := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	authority := Authority{
		Surface:        requestorigin.SurfaceCustomDomain,
		BaseURL:        "https://custom.example.com",
		OrganizationID: "org_123",
		NamespaceKind:  NamespaceCustomDomain,
		CustomDomainID: domainID,
	}
	require.NoError(t, authority.ValidateEndpointRef(authority.BaseURL, domainID))
	require.Error(t, authority.ValidateEndpointRef("https://other.example.com", domainID))
	require.Error(t, authority.ValidateEndpointRef(authority.BaseURL, uuid.NullUUID{}))

	platform := Authority{
		Surface:        requestorigin.SurfacePlatform,
		BaseURL:        "https://platform.example.com",
		OrganizationID: "org_123",
		NamespaceKind:  NamespacePlatform,
	}
	require.NoError(t, platform.ValidateEndpointRef(platform.BaseURL, uuid.NullUUID{UUID: uuid.New(), Valid: true}), "platform authority ignores a legacy toolset's stored custom-domain binding")
}

func TestValidateBaseURLCanonicalizesPrivateOrigin(t *testing.T) {
	t.Parallel()
	authority := Authority{Surface: requestorigin.SurfacePrivateNetwork, BaseURL: "https://private.example.ts.net"}
	require.NoError(t, authority.ValidateBaseURL("https://PRIVATE.EXAMPLE.TS.NET"))
	require.Error(t, authority.ValidateBaseURL("https://private.example.ts.net:8443"))
	require.Error(t, authority.ValidateBaseURL("http://private.example.ts.net"))
}

func TestAuthorityJSONContainsNoIdentityOrCredentials(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(Authority{
		Surface:          requestorigin.SurfacePrivateNetwork,
		BaseURL:          "https://private.example.ts.net",
		OrganizationID:   "org_123",
		NetworkIngressID: uuid.New(),
		NamespaceKind:    NamespacePlatform,
	})
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "identity")
	require.NotContains(t, string(encoded), "credential")
	require.NotContains(t, string(encoded), "login")
}

func TestValidateLiveRejectsDisabledDeletedAndMismatchedAuthority(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "networkingressauthority")
	require.NoError(t, err)
	ingressID := insertIngress(t, ctx, db)
	valid := Authority{
		Surface:          requestorigin.SurfacePrivateNetwork,
		BaseURL:          "https://private.example.ts.net",
		OrganizationID:   "org_authority",
		NetworkIngressID: ingressID,
		NamespaceKind:    NamespacePlatform,
		CustomDomainID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}
	require.NoError(t, valid.ValidateLive(ctx, db))

	wrongOrg := valid
	wrongOrg.OrganizationID = "org_other"
	require.Error(t, wrongOrg.ValidateLive(ctx, db))
	wrongNamespace := valid
	wrongNamespace.NamespaceKind = NamespaceCustomDomain
	wrongNamespace.CustomDomainID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	require.Error(t, wrongNamespace.ValidateLive(ctx, db))
	wrongOrigin := valid
	wrongOrigin.BaseURL = "https://other.example.ts.net"
	require.Error(t, wrongOrigin.ValidateLive(ctx, db))

	fixtures := testrepo.New(db)
	require.NoError(t, fixtures.SetNetworkIngressEnabledFixture(ctx, testrepo.SetNetworkIngressEnabledFixtureParams{Enabled: false, ID: ingressID}))
	require.Error(t, valid.ValidateLive(ctx, db))
	require.NoError(t, fixtures.SetNetworkIngressEnabledFixture(ctx, testrepo.SetNetworkIngressEnabledFixtureParams{Enabled: true, ID: ingressID}))
	require.NoError(t, fixtures.SoftDeleteNetworkIngressFixture(ctx, ingressID))
	require.Error(t, valid.ValidateLive(ctx, db))
}

func insertIngress(t *testing.T, ctx context.Context, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ingressID := uuid.New()
	require.NoError(t, testrepo.New(db).InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{
		ID:             ingressID,
		OrganizationID: "org_authority",
		DnsName:        pgtype.Text{String: "private.example.ts.net", Valid: true},
	}))
	return ingressID
}

func TestGetLiveNetworkIngressAuthorityReturnsPinnedFields(t *testing.T) {
	t.Parallel()
	ingressID := uuid.New()
	domainID := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	row := &authorityRow{values: []any{ingressID, "org_123", NamespaceCustomDomain, domainID, pgtype.Text{String: "private.example.ts.net", Valid: true}}}
	got, err := repo.New(authorityDB{row: row}).GetLiveNetworkIngressAuthority(t.Context(), ingressID)
	require.NoError(t, err)
	require.Equal(t, ingressID, got.ID)
	require.Equal(t, "org_123", got.OrganizationID)
	require.Equal(t, NamespaceCustomDomain, got.EndpointNamespaceKind)
	require.Equal(t, domainID, got.CustomDomainID)
}

type authorityDB struct{ row pgx.Row }

func (d authorityDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (d authorityDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (d authorityDB) QueryRow(context.Context, string, ...any) pgx.Row        { return d.row }

type authorityRow struct {
	values []any
	err    error
}

func (r *authorityRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*uuid.UUID) = r.values[0].(uuid.UUID)         //nolint:forcetypeassert // fixed sqlc scan contract
	*dest[1].(*string) = r.values[1].(string)               //nolint:forcetypeassert // fixed sqlc scan contract
	*dest[2].(*string) = r.values[2].(string)               //nolint:forcetypeassert // fixed sqlc scan contract
	*dest[3].(*uuid.NullUUID) = r.values[3].(uuid.NullUUID) //nolint:forcetypeassert // fixed sqlc scan contract
	*dest[4].(*pgtype.Text) = r.values[4].(pgtype.Text)     //nolint:forcetypeassert // fixed sqlc scan contract
	return nil
}

func TestValidateNamespaceFailsClosed(t *testing.T) {
	t.Parallel()
	require.NoError(t, validateNamespace(NamespacePlatform, uuid.NullUUID{}))
	require.Error(t, validateNamespace(NamespacePlatform, uuid.NullUUID{UUID: uuid.New(), Valid: true}))
	require.Error(t, validateNamespace(NamespaceCustomDomain, uuid.NullUUID{}))
	require.Error(t, validateNamespace("unknown", uuid.NullUUID{}))
}
