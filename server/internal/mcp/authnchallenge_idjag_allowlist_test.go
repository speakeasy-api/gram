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

// newIDJAGAllowlistFixture is a toolset on a trusted ID-JAG issuer whose
// guardian resolves no host, so no client metadata document can be fetched.
func newIDJAGAllowlistFixture(t *testing.T) (context.Context, *testInstance, usersessionsrepo.UserSessionIssuer, string) {
	t.Helper()
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
	return ctx, ti, issuer, toolset.McpSlug.String
}

func TestTokenIDJAGExchangeRefusesNormalizedVariantsOfListedClient(t *testing.T) {
	t.Parallel()

	ctx, ti, issuer, slug := newIDJAGAllowlistFixture(t)
	for _, clientID := range []string{
		codexCIMDClientID + "/",
		"https://CHATGPT.com/oauth/codex/client.json",
		"https://chatgpt.com:443/oauth/codex/client.json",
		codexCIMDClientID + "?v=1",
		codexCIMDClientID + "#client",
	} {
		w := postIDJAGToken(t, ctx, ti, slug, clientID, "unused.assertion.value")
		require.Equal(t, http.StatusBadRequest, w.Code, clientID)
		require.JSONEq(t, `{"error":"unauthorized_client","error_description":"this client is not allowed to use the ID-JAG grant"}`, w.Body.String(), clientID)
		_, err := usersessionsrepo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessionsrepo.GetUserSessionClientByClientIDParams{UserSessionIssuerID: issuer.ID, ClientID: clientID})
		require.Error(t, err, "a refused client_id must not be stored: %s", clientID)
	}
}

// A registered row cannot pose as a listed document: the token endpoint
// refetches the document for it and never serves the row as CIMD-resolved.
func TestTokenIDJAGExchangeRefusesRegisteredRowHoldingListedClientID(t *testing.T) {
	t.Parallel()

	ctx, ti, issuer, slug := newIDJAGAllowlistFixture(t)
	_, err := usersessionsrepo.New(ti.conn).CreateUserSessionClient(ctx, usersessionsrepo.CreateUserSessionClientParams{
		UserSessionIssuerID:     issuer.ID,
		ClientID:                codexCIMDClientID,
		ClientName:              "Registered collision",
		RedirectUris:            []string{"http://127.0.0.1/callback"},
		TokenEndpointAuthMethod: oauthwire.AuthMethodNone,
	})
	require.NoError(t, err)

	w := postIDJAGToken(t, ctx, ti, slug, codexCIMDClientID, "unused.assertion.value")
	require.NotEqual(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "access_token")

	row, err := usersessionsrepo.New(ti.conn).GetUserSessionClientByClientID(ctx, usersessionsrepo.GetUserSessionClientByClientIDParams{UserSessionIssuerID: issuer.ID, ClientID: codexCIMDClientID})
	require.NoError(t, err)
	require.False(t, row.ClientIDMetadataUri.Valid)
}
