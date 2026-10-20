package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// An unnamed tenant issuer shadows the catalog issuer for the same
// authorization server; the consent card still gets the catalog name.
func TestWithCatalogBranding_UnnamedTenantIssuerBorrowsCatalogName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	_, err := repo.New(ti.conn).CreateRemoteSessionIssuer(ctx, repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{},
		OrganizationID:                    pgtype.Text{String: "", Valid: false},
		Slug:                              "catalog-idp",
		Name:                              conv.ToPGText("Example IdP"),
		Issuer:                            "https://idp.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://idp.example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
	})
	require.NoError(t, err)

	tenantIssuer := seedOrgLevelRemoteIssuer(t, ctx, ti.conn, authCtx.ActiveOrganizationID, "unnamed-tenant-idp")
	userIssuer := createUserSessionIssuer(t, ctx, ti.conn, "catalog-branding-usi")
	seedOrgLevelRemoteClient(t, ctx, ti.conn, authCtx.ActiveOrganizationID, tenantIssuer, "branding-cid", userIssuer)

	mgr := newResolveManager(t, ti.conn, testenv.NewEncryptionClient(t))
	clients, err := mgr.ListClients(ctx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, userIssuer)
	require.NoError(t, err)
	require.Len(t, clients, 1)
	require.Nil(t, clients[0].IssuerName)

	branded := mgr.WithCatalogBranding(ctx, clients)
	require.Len(t, branded, 1)
	require.NotNil(t, branded[0].IssuerName)
	require.Equal(t, "Example IdP", *branded[0].IssuerName)
}
