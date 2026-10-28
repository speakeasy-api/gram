package mcp_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// A registered row holding a catalog document URL is never served as that
// document: the token endpoint refetches it, here unreachable, and mints nothing.
func TestTokenIDJAGExchangeRefusesRegisteredRowHoldingCatalogClientID(t *testing.T) {
	t.Parallel()

	noDNS := dns.NewMockResolver(dns.MockResolverConfig{
		LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) {
			return nil, errors.New("no DNS in this test")
		},
	})
	ctx, ti := newTestMCPServiceWithMeterProviderAndGuardianOptions(t, testenv.NewMeterProvider(t), guardian.WithResolver(noDNS))
	authCtx := requireProjectAuthContext(t, ctx)
	toolset, _, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	issuer := createTrustedIDJAGIssuer(t, ctx, ti, authCtx.ActiveOrganizationID, "https://identity.example.test", "https://identity.example.test/keys")
	toolset, err := toolsetsrepo.New(ti.conn).UpdateToolsetUserSessionIssuer(ctx, toolsetsrepo.UpdateToolsetUserSessionIssuerParams{
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		Slug:                toolset.Slug,
		ProjectID:           *authCtx.ProjectID,
	})
	require.NoError(t, err)
	_, err = usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuer.ID,
		ClientID:                codexCIMDClientID,
		ClientName:              "Registered collision",
		RedirectUris:            []string{"http://127.0.0.1/callback"},
		TokenEndpointAuthMethod: oauthwire.AuthMethodNone,
	})
	require.NoError(t, err)

	w := postIDJAGToken(t, ctx, ti, toolset.McpSlug.String, codexCIMDClientID, "unused.assertion.value")
	require.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "access_token")

	row, err := usersessionsrepo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessionsrepo.GetUserSessionClientByClientIDParams{UserSessionIssuerID: issuer.ID, ClientID: codexCIMDClientID})
	require.NoError(t, err)
	require.Equal(t, "Registered collision", row.ClientName)
	require.False(t, row.ClientIDMetadataUri.Valid)
}
