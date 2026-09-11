package networkingress

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

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

func TestGetLiveNetworkIngressAuthorityReturnsPinnedFields(t *testing.T) {
	t.Parallel()
	ingressID := uuid.New()
	domainID := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	row := &authorityRow{values: []any{ingressID, "org_123", NamespaceCustomDomain, domainID, pgtype.Text{String: "private.example.ts.net", Valid: true}}}
	got, err := repo.New(authorityDB{row: row}).GetLiveNetworkIngressAuthority(t.Context(), repo.GetLiveNetworkIngressAuthorityParams{
		ID: ingressID, OrganizationID: "org_123",
	})
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

func TestPrivateBaseURLCanonicalizesIPv6(t *testing.T) {
	t.Parallel()

	got, err := canonicalPrivateBaseURL("https://[2001:DB8::1]")
	require.NoError(t, err)
	require.Equal(t, "https://[2001:db8::1]", got)
	require.Equal(t, "https://[2001:db8::1]", expectedBaseURL(pgtype.Text{String: "2001:db8::1", Valid: true}))
}

func TestValidateNamespaceFailsClosed(t *testing.T) {
	t.Parallel()
	require.NoError(t, validateNamespace(NamespacePlatform, uuid.NullUUID{}))
	require.Error(t, validateNamespace(NamespacePlatform, uuid.NullUUID{UUID: uuid.New(), Valid: true}))
	require.Error(t, validateNamespace(NamespaceCustomDomain, uuid.NullUUID{}))
	require.Error(t, validateNamespace("unknown", uuid.NullUUID{}))
}
