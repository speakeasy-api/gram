package mcp_test

import (
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// An unnamed tenant issuer shadowing a catalog issuer for the same
// authorization server renders the catalog name, on the card and in the
// stored inactive reason alike.
func TestServeConsent_UnnamedIssuerBorrowsCatalogName(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMetaValidationFixture(t, "catbrand")
	projectID, orgID := consentTestTenant(t, ctx)

	// The trailing slash is the catalog's spelling of the fixture's issuer.
	_, err := remotesessions_repo.New(fx.ti.conn).CreateRemoteSessionIssuer(ctx, remotesessions_repo.CreateRemoteSessionIssuerParams{
		ProjectID:                         uuid.NullUUID{},
		OrganizationID:                    pgtype.Text{String: "", Valid: false},
		Slug:                              "catbrand-catalog",
		Name:                              conv.ToPGText("Example Catalog"),
		Issuer:                            "https://catbrand-as.example.com/",
		AuthorizationEndpoint:             conv.ToPGText("https://catbrand-as.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://catbrand-as.example.com/token"),
		ScopesSupported:                   []string{},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		CodeChallengeMethodsSupported:     []string{"S256"},
	})
	require.NoError(t, err)

	page := renderConsent(t, fx)
	require.Contains(t, page, `data-remote-display="Example Catalog"`)
	require.NotContains(t, page, "catbrand-rsi")

	var body atomic.Pointer[string]
	body.Store(conv.PtrEmpty(`{"active":false}`))
	rows, err := testrepo.New(fx.ti.conn).ForceRemoteSessionIssuerEnrichmentEndpointsFixture(ctx, testrepo.ForceRemoteSessionIssuerEnrichmentEndpointsFixtureParams{
		UserinfoEndpoint:      pgtype.Text{String: "", Valid: false},
		IntrospectionEndpoint: conv.ToPGText(introspectionServer(t, &body)),
		JwksUri:               pgtype.Text{String: "", Valid: false},
		RemoteSessionClientID: fx.clientID,
		ProjectID:             projectID,
		OrganizationID:        orgID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)

	fx.member.set(memberRejects)
	requireValidated(t, fx)
	require.Equal(t, "Inactive at Example Catalog", storedSession(t, ctx, fx).ValidationReason.String)
	require.Contains(t, renderConsent(t, fx), "Inactive at Example Catalog — reconnect to continue")
}
